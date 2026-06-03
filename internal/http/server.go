package httpserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/FelippeRibeiro/crypter-drive/internal/auth"
	"github.com/FelippeRibeiro/crypter-drive/internal/drive"
	"github.com/FelippeRibeiro/crypter-drive/internal/encrypter"
)

type Server struct {
	db        *sql.DB
	auth      *auth.Service
	drive     *drive.Service
	masterKey []byte
	webDir    string
}

func New(db *sql.DB, authSvc *auth.Service, driveSvc *drive.Service, masterKey []byte, webDir string) *Server {
	return &Server{
		db:        db,
		auth:      authSvc,
		drive:     driveSvc,
		masterKey: masterKey,
		webDir:    webDir,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/auth/register", s.handleRegister)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/vaults", s.handleListVaults)
	protected.HandleFunc("POST /api/vaults", s.handleCreateVault)
	protected.HandleFunc("GET /api/vaults/{vaultID}/files", s.handleListVaultFiles)
	protected.HandleFunc("POST /api/vaults/{vaultID}/files", s.handleUploadVaultFile)
	protected.HandleFunc("GET /api/files/{fileID}/download", s.handleDownloadFile)
	protected.HandleFunc("DELETE /api/files/{fileID}", s.handleDeleteFile)

	mux.Handle("/api/vaults", s.auth.Middleware(protected))
	mux.Handle("/api/vaults/", s.auth.Middleware(protected))
	mux.Handle("/api/files/", s.auth.Middleware(protected))

	fileServer := http.FileServer(http.Dir(s.webDir))
	mux.Handle("/", fileServer)

	return withJSONContentType(mux)
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string `json:"token"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "email and password(>=8) are required")
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	var userID int64
	err = s.db.QueryRowContext(
		r.Context(),
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		req.Email,
		passwordHash,
	).Scan(&userID)
	if err != nil {
		if strings.Contains(err.Error(), "users_email_key") {
			writeError(w, http.StatusConflict, "email already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	token, err := s.auth.Generate(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}
	writeJSON(w, http.StatusCreated, authResponse{Token: token})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	var userID int64
	var passwordHash string
	err := s.db.QueryRowContext(
		r.Context(),
		`SELECT id, password_hash FROM users WHERE email = $1`,
		req.Email,
	).Scan(&userID, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to fetch user")
		return
	}

	if err := auth.CheckPassword(passwordHash, req.Password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := s.auth.Generate(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token})
}

type vaultResponse struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Server) handleListVaults(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT id, name, created_at
		FROM vaults
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list vaults")
		return
	}
	defer rows.Close()

	vaults := make([]vaultResponse, 0)
	for rows.Next() {
		var v vaultResponse
		if err := rows.Scan(&v.ID, &v.Name, &v.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read vault data")
			return
		}
		vaults = append(vaults, v)
	}
	writeJSON(w, http.StatusOK, vaults)
}

type createVaultRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateVault(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createVaultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "vault name is required")
		return
	}

	vaultKey, err := encrypter.GenerateVaultKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate vault key")
		return
	}
	keyNonce, keyCipher, err := encrypter.WrapVaultKey(s.masterKey, vaultKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to protect vault key")
		return
	}

	var resp vaultResponse
	err = s.db.QueryRowContext(r.Context(), `
		INSERT INTO vaults (user_id, name, key_nonce, key_cipher)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, created_at
	`, userID, req.Name, keyNonce, keyCipher).Scan(&resp.ID, &resp.Name, &resp.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "vaults_user_id_name_key") {
			writeError(w, http.StatusConflict, "vault name already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create vault")
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

type fileResponse struct {
	ID               int64     `json:"id"`
	OriginalFileName string    `json:"originalFileName"`
	DriveFileID      string    `json:"driveFileId"`
	CreatedAt        time.Time `json:"createdAt"`
	SizeBytes        int64     `json:"sizeBytes"`
}

func (s *Server) handleListVaultFiles(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	vaultID, err := strconv.ParseInt(r.PathValue("vaultID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid vault id")
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT f.id, f.original_file_name, f.size_bytes, f.created_at, m.drive_file_id
		FROM files f
		INNER JOIN file_mappings m ON m.file_id = f.id
		WHERE f.user_id = $1 AND f.vault_id = $2
		ORDER BY f.created_at DESC
	`, userID, vaultID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list files")
		return
	}
	defer rows.Close()

	files := make([]fileResponse, 0)
	for rows.Next() {
		var f fileResponse
		if err := rows.Scan(&f.ID, &f.OriginalFileName, &f.SizeBytes, &f.CreatedAt, &f.DriveFileID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read file data")
			return
		}
		files = append(files, f)
	}
	writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleUploadVaultFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	vaultID, err := strconv.ParseInt(r.PathValue("vaultID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid vault id")
		return
	}

	vaultKey, err := s.loadVaultKey(r.Context(), userID, vaultID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "vault not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load vault key")
		return
	}

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart request")
		return
	}

	inputFile, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer inputFile.Close()

	encryptedStream, err := encrypter.EncryptReader(inputFile, vaultKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encrypt file")
		return
	}

	driveName := uuid.NewString() + ".bin"
	if ext := strings.TrimSpace(filepath.Ext(header.Filename)); ext != "" && len(ext) <= 8 {
		driveName = uuid.NewString() + ext + ".enc"
	}

	uploaded, err := s.drive.UploadEncrypted(r.Context(), driveName, encryptedStream)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to upload to google drive")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open transaction")
		return
	}
	defer tx.Rollback()

	var fileID int64
	err = tx.QueryRowContext(r.Context(), `
		INSERT INTO files (user_id, vault_id, original_file_name, mime_type, size_bytes)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, userID, vaultID, header.Filename, header.Header.Get("Content-Type"), header.Size).Scan(&fileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist file metadata")
		return
	}

	_, err = tx.ExecContext(r.Context(), `
		INSERT INTO file_mappings (file_id, drive_file_id, drive_file_name, drive_folder_id)
		VALUES ($1, $2, $3, $4)
	`, fileID, uploaded.ID, uploaded.Name, uploaded.FolderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist file mapping")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"fileId":           fileID,
		"originalFileName": header.Filename,
		"driveFileId":      uploaded.ID,
	})
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	fileID, err := strconv.ParseInt(r.PathValue("fileID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}

	meta, err := s.fetchFileForDownload(r.Context(), userID, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load file metadata")
		return
	}

	vaultKey, err := encrypter.UnwrapVaultKey(s.masterKey, meta.KeyNonce, meta.KeyCipher)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unwrap vault key")
		return
	}

	encryptedBody, err := s.drive.Download(r.Context(), meta.DriveFileID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to download from drive")
		return
	}
	defer encryptedBody.Close()

	decryptedReader, err := encrypter.DecryptReader(encryptedBody, vaultKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decrypt file")
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.OriginalFileName))
	if meta.MimeType != "" {
		w.Header().Set("Content-Type", meta.MimeType)
	}
	if _, err := io.Copy(w, decryptedReader); err != nil {
		return
	}
}

func (s *Server) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	fileID, err := strconv.ParseInt(r.PathValue("fileID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid file id")
		return
	}

	meta, err := s.fetchFileForDownload(r.Context(), userID, fileID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load file metadata")
		return
	}

	if err := s.drive.DeleteFile(r.Context(), meta.DriveFileID); err != nil {
		writeError(w, http.StatusBadGateway, "failed to delete file from drive")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to open transaction")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM file_mappings WHERE file_id = $1`, fileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete file mapping")
		return
	}
	result, err := tx.ExecContext(r.Context(), `DELETE FROM files WHERE id = $1 AND user_id = $2`, fileID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete file")
		return
	}
	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit delete")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"fileId":  fileID,
	})
}

type fileDownloadMeta struct {
	OriginalFileName string
	DriveFileID      string
	MimeType         string
	KeyNonce         []byte
	KeyCipher        []byte
}

func (s *Server) fetchFileForDownload(ctx context.Context, userID, fileID int64) (fileDownloadMeta, error) {
	var meta fileDownloadMeta
	err := s.db.QueryRowContext(ctx, `
		SELECT f.original_file_name, f.mime_type, m.drive_file_id, v.key_nonce, v.key_cipher
		FROM files f
		INNER JOIN file_mappings m ON m.file_id = f.id
		INNER JOIN vaults v ON v.id = f.vault_id
		WHERE f.id = $1 AND f.user_id = $2
	`, fileID, userID).Scan(
		&meta.OriginalFileName,
		&meta.MimeType,
		&meta.DriveFileID,
		&meta.KeyNonce,
		&meta.KeyCipher,
	)
	return meta, err
}

func (s *Server) loadVaultKey(ctx context.Context, userID, vaultID int64) ([]byte, error) {
	var nonce, cipher []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT key_nonce, key_cipher
		FROM vaults
		WHERE id = $1 AND user_id = $2
	`, vaultID, userID).Scan(&nonce, &cipher)
	if err != nil {
		return nil, err
	}
	return encrypter.UnwrapVaultKey(s.masterKey, nonce, cipher)
}

func withJSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
