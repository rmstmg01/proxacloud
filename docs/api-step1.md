# Step 1 API Endpoints

Base URL: `http://localhost:8080`

## Health

- `GET /health`

## Auth

- `POST /api/v1/auth/login`
- `GET /api/v1/auth/me`

## Users

- `GET /api/v1/users` (`super_user`, `super_admin`)
- `POST /api/v1/users` (`super_admin`)

## Networks

- `GET /api/v1/networks` (any authenticated user)
- `POST /api/v1/networks` (`super_user`, `super_admin`)
- `PUT /api/v1/networks/{id}` (`super_user`, `super_admin`)
- `POST /api/v1/networks/{id}/set-default` (`super_user`, `super_admin`)

## Quick Test

### 1) Login

```powershell
$body = @{ username = "k8s-admin"; password = "ChangeMeNow!" } | ConvertTo-Json
$resp = Invoke-RestMethod -Method Post -Uri "http://localhost:8080/api/v1/auth/login" -ContentType "application/json" -Body $body
$token = $resp.token
```

### 2) List Networks

```powershell
Invoke-RestMethod -Method Get -Uri "http://localhost:8080/api/v1/networks" -Headers @{ Authorization = "Bearer $token" }
```
