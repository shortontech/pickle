# Requests

Request classes define incoming JSON payloads with validation rules. Pickle generates `Bind` functions that deserialize and validate in one step.

## Writing a request

```go
// app/http/requests/create_user.go
package requests

type CreateUserRequest struct {
    Name     string `json:"name" validate:"required,min=2,max=100"`
    Email    string `json:"email" validate:"required,email"`
    Password string `json:"password" validate:"required,min=8"`
    Role     string `json:"role" validate:"omitempty,oneof=user admin"`
}
```

## Validation tags

Pickle uses `github.com/go-playground/validator/v10` for struct validation. Common tags:

| Tag | Description |
|-----|-------------|
| `required` | Field must be present and non-zero |
| `email` | Must be valid email format |
| `min=N` | Minimum length (string) or value (number) |
| `max=N` | Maximum length or value |
| `oneof=a b c` | Must be one of the listed values |
| `uuid` | Must be valid UUID |
| `url` | Must be valid URL |
| `omitempty` | Skip validation if field is zero value |

Combine with commas: `validate:"required,email"`, `validate:"required,min=1,max=100"`.

## Generated binding

Pickle generates a `Bind` function for each request struct:

```go
// requests/bindings_gen.go (GENERATED)
func BindCreateUserRequest(r *http.Request) (CreateUserRequest, *BindingError)
```

## Using in controllers

```go
func (c UserController) Store(ctx *pickle.Context) pickle.Response {
    req, bindErr := requests.BindCreateUserRequest(ctx.Request())
    if bindErr != nil {
        return ctx.JSON(bindErr.Status, bindErr)
    }

    // req is a validated CreateUserRequest — use it safely
    user := &models.User{
        Name:  req.Name,
        Email: req.Email,
    }
    // ...
}
```

## BindingError

When binding fails, the `*BindingError` contains:

```go
type BindingError struct {
    Status  int               `json:"status"`
    Message string            `json:"message"`
    Errors  map[string]string `json:"errors,omitempty"`
}
```

- JSON parse errors → `400` with a message
- Validation errors → `422` with a map of field → error message

Example error response:

```json
{
    "status": 422,
    "message": "validation failed",
    "errors": {
        "email": "must be a valid email address",
        "password": "must be at least 8 characters"
    }
}
```

## Mass assignment protection

Only fields defined in the request struct are deserialized. POSTing `{"role": "admin"}` does nothing if the request struct doesn't have a `Role` field. This is structural protection — there's no way to bypass it.

## Request location

Request files live in `app/http/requests/`. One file per request, named after the operation: `create_user.go`, `update_user.go`, `login.go`.
