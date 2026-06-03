package drive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type Service struct {
	api      *drive.Service
	rootName string

	mu           sync.Mutex
	cachedRootID string
}

type FileUpload struct {
	ID       string
	Name     string
	FolderID string
}

func getCodeFromWeb() (string, error) {
	epServer := http.NewServeMux()
	codeChan := make(chan string)

	epServer.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			fmt.Fprintf(w, "Erro na autorização: %s. Pode fechar esta janela.", errParam)
			codeChan <- ""
			return
		}
		code := r.URL.Query().Get("code")
		fmt.Fprintf(w, "Autenticação concluída com sucesso! Volte para o terminal.")
		codeChan <- code
	})
	server := &http.Server{Addr: ":3134", Handler: epServer}

	go func() {
		fmt.Println("Server started at")
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			codeChan <- ""
		}
	}()
	code := <-codeChan
	_ = server.Shutdown(context.Background())
	if code == "" {
		return "", errors.New("fluxo de autenticação abortado ou falhou")
	}
	return code, nil
}

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config, tokenFile string) (*http.Client, error) {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tok, err := tokenFromFile(tokenFile)
	if err != nil {
		tok, err = getTokenFromWeb(config)
		if err != nil {
			return nil, err
		}
		if err := saveToken(tokenFile, tok); err != nil {
			return nil, err
		}
	}

	tokenSource := config.TokenSource(context.Background(), tok)
	validatedToken, err := tokenSource.Token()
	if err != nil {
		if isInvalidGrant(err) {
			_ = os.Remove(tokenFile)
			tok, err = getTokenFromWeb(config)
			if err != nil {
				return nil, err
			}
			if err := saveToken(tokenFile, tok); err != nil {
				return nil, err
			}
			tokenSource = config.TokenSource(context.Background(), tok)
			validatedToken, err = tokenSource.Token()
			if err != nil {
				return nil, fmt.Errorf("oauth token validation failed after reauth: %w", err)
			}
		} else {
			return nil, fmt.Errorf("oauth token validation failed: %w", err)
		}
	}
	return config.Client(context.Background(), validatedToken), nil
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) (*oauth2.Token, error) {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the authorization code:\n%v\n", authURL)

	authCode, err := getCodeFromWeb()
	if err != nil {
		return nil, err
	}
	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve token from web: %w", err)
	}
	return tok, nil
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("unable to cache oauth token: %w", err)
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(token); err != nil {
		return fmt.Errorf("unable to encode oauth token: %w", err)
	}
	return nil
}

func NewService(credentialsFile, tokenFile, rootName string) (*Service, error) {
	ctx := context.Background()
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read client secret file: %w", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, drive.DriveScope)

	if err != nil {
		return nil, fmt.Errorf("unable to parse client secret file to config: %w", err)
	}

	client, err := getClient(config, tokenFile)
	if err != nil {
		return nil, fmt.Errorf("unable to create oauth client: %w", err)
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))

	if err != nil {
		return nil, fmt.Errorf("unable to retrieve drive client: %w", err)
	}

	return &Service{api: srv, rootName: rootName}, nil
}

func (s *Service) EnsureRootFolder(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cachedRootID != "" {
		return s.cachedRootID, nil
	}

	query := fmt.Sprintf(
		"mimeType='application/vnd.google-apps.folder' and name='%s' and trashed=false",
		escapeDriveQueryValue(s.rootName),
	)
	list, err := s.api.Files.List().
		Context(ctx).
		Q(query).
		Fields("files(id,name)").
		PageSize(1).
		Do()
	if err != nil {
		return "", err
	}
	if len(list.Files) > 0 {
		s.cachedRootID = list.Files[0].Id
		return s.cachedRootID, nil
	}

	folder := &drive.File{
		Name:     s.rootName,
		MimeType: "application/vnd.google-apps.folder",
	}
	created, err := s.api.Files.Create(folder).Context(ctx).Fields("id").Do()
	if err != nil {
		return "", err
	}
	s.cachedRootID = created.Id
	return s.cachedRootID, nil
}

func (s *Service) UploadEncrypted(ctx context.Context, driveFileName string, content io.Reader) (*FileUpload, error) {
	rootID, err := s.EnsureRootFolder(ctx)
	if err != nil {
		return nil, err
	}

	file := &drive.File{
		Name:    driveFileName,
		Parents: []string{rootID},
	}

	call := s.api.Files.Create(file).Context(ctx).Fields("id,name,parents")
	call = call.Media(content)
	created, err := call.Do()
	if err != nil {
		return nil, err
	}

	return &FileUpload{
		ID:       created.Id,
		Name:     created.Name,
		FolderID: rootID,
	}, nil
}

func (s *Service) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	resp, err := s.api.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (s *Service) DeleteFile(ctx context.Context, fileID string) error {
	return s.api.Files.Delete(fileID).Context(ctx).Do()
}

func escapeDriveQueryValue(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

func isInvalidGrant(err error) bool {
	return strings.Contains(err.Error(), "invalid_grant")
}
