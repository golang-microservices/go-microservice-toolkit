package authentication

import (
	"context"
	"net/http"

	jwt "github.com/dgrijalva/jwt-go"
)

// Authenticate is a default authentication middleware to enforce access from the
// Verifier middleware request context values. The Authenticate sends a 401 Unauthorized
// response for any unverified tokens and passes the good ones through. It's just fine
// until you decide to write something similar and customize your client response.
func (ja *jwtAuth) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, claims, err := TokenFromContext(r.Context())

		if err != nil {
			http.Error(w, http.StatusText(401), 401)
			return
		}

		if token == nil || !token.Valid {
			http.Error(w, http.StatusText(401), 401)
			return
		}

		// Token is authenticated, parse claims
		var c AppClaims
		err = c.ParseClaims(claims)
		if err != nil {
			http.Error(w, http.StatusText(401), 401)
			return
		}

		// Set AppClaims on context
		ctx := context.WithValue(r.Context(), AccessClaimsCtxKey, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Verify http middleware handler will verify a JWT string from a http request.
//
// Verify will search for a JWT token in a http request, in the order:
//   1. 'jwt' URI query parameter
//   2. 'Authorization: BEARER T' request header
//   3. Cookie 'jwt' value
//
// The first JWT string that is found as a query parameter, authorization header
// or cookie header is then decoded by the `jwt-go` library and a *jwt.Token
// object is set on the request context. In the case of a signature decoding error
// the Verify will also set the error on the request context.
//
// The Verify always calls the next http handler in sequence, which can either
// be the generic `jwtauth.Authenticate` middleware or your own custom handler
// which checks the request context jwt token and error to prepare a custom
// http response.
func (ja *jwtAuth) Verify() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return ja.verify(ja.TokenFromQuery, ja.TokenFromHeader, ja.TokenFromCookie)(next)
	}
}

func (ja *jwtAuth) verify(findTokenFns ...func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		hfn := func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			token, err := ja.verifyRequest(r, findTokenFns...)
			ctx = NewContext(ctx, token, err)
			next.ServeHTTP(w, r.WithContext(ctx))
		}
		return http.HandlerFunc(hfn)
	}
}

func (ja *jwtAuth) verifyRequest(r *http.Request, findTokenFns ...func(r *http.Request) string) (*jwt.Token, error) {
	var tokenStr string
	var err error

	// Extract token string from the request by calling token find functions in
	// the order they where provided. Further extraction stops if a function
	// returns a non-empty string.
	for _, fn := range findTokenFns {
		tokenStr = fn(r)
		if tokenStr != "" {
			break
		}
	}
	if tokenStr == "" {
		return nil, ErrNoTokenFound
	}

	// Verify the token
	token, err := ja.Decode(tokenStr)
	if err != nil {
		if verr, ok := err.(*jwt.ValidationError); ok {
			if verr.Errors&jwt.ValidationErrorExpired > 0 {
				return token, ErrExpired
			} else if verr.Errors&jwt.ValidationErrorIssuedAt > 0 {
				return token, ErrIATInvalid
			} else if verr.Errors&jwt.ValidationErrorNotValidYet > 0 {
				return token, ErrNBFInvalid
			}
		}
		return token, err
	}

	// Verify signing algorithm
	if token.Method != ja.signer {
		return token, ErrAlgoInvalid
	}

	// Valid!
	return token, nil
}

// RequiresRole middleware restricts access to accounts having role parameter in their jwt claims.
func (ja *jwtAuth) RequiresRole(role Role) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		hfn := func(w http.ResponseWriter, r *http.Request) {
			claims := AppClaimsFromCtx(r.Context())
			if !hasRole(role, claims.Roles) {
				http.Error(w, http.StatusText(401), 401)
				return
			}
			next.ServeHTTP(w, r)
		}
		return http.HandlerFunc(hfn)
	}
}

func hasRole(role Role, roles []Role) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}
