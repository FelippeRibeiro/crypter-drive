# Crypter Drive

Armazenador de arquivos criptografados no Google Drive com:

- API REST em Go
- Frontend web simples
- PostgreSQL para contas/cofres/mapeamento
- Criptografia por cofre
- Pasta automática `crypter` no Google Drive

## Requisitos

- Docker e Docker Compose
- Credenciais OAuth do Google Drive (`credentials.json`)
- Token OAuth válido (`token.json`)

## Configuração

1. Copie o ambiente:
   - `cp .env.example .env`
2. Gere `MASTER_KEY_BASE64` (32 bytes):
   - `openssl rand -base64 32`
3. Garanta os arquivos na raiz do projeto:
   - `credentials.json`
   - `token.json`

## Subir a aplicação

```bash
docker compose up --build
```

Aplicação disponível em:

- Web UI: `http://localhost:8080`
- API: `http://localhost:8080/api`

## Fluxo funcional

1. Criar conta ou entrar na UI.
2. Criar um cofre.
3. Fazer upload de qualquer arquivo.
4. Arquivo é criptografado e salvo no Drive com nome randomizado.
5. A aplicação lista nomes originais somente para o dono autenticado.
6. A aplicação permite apagar arquivo (remove do Google Drive e do banco).

## OAuth e token inválido (`invalid_grant`)

Se aparecer erro `oauth2: "invalid_grant"`:

1. Remova `token.json`.
2. Suba novamente (`docker compose up --build`).
3. Abra no navegador o link de autorização impresso nos logs da aplicação.
4. Conclua o login Google; o callback usa a porta `3134` (já exposta no `docker-compose`).

## Smoke tests (manual)

Com a aplicação rodando:

1. Registro:
   - `POST /api/auth/register`
2. Login:
   - `POST /api/auth/login`
3. Criar cofre autenticado:
   - `POST /api/vaults`
4. Upload autenticado multipart:
   - `POST /api/vaults/{vaultID}/files`
5. Listagem autenticada:
   - `GET /api/vaults/{vaultID}/files`
6. Download descriptografado:
   - `GET /api/files/{fileID}/download`
7. Exclusão autenticada:
   - `DELETE /api/files/{fileID}`

## Observações de segurança

- Nome original do arquivo não é usado no Google Drive.
- A referência principal remota é `drive_file_id`.
- A chave de cada cofre é gerada aleatoriamente e armazenada protegida por `MASTER_KEY_BASE64`.
