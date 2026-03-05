package grpc

import (
	"encoding/base64"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// NewStreamAuthInterceptor creates a gRPC stream server interceptor that validates Basic auth credentials
func NewStreamAuthInterceptor(username, password string) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Extract metadata from incoming request
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return status.Error(codes.Unauthenticated, "missing metadata")
		}

		// Get Authorization header
		authValues := md.Get("authorization")
		if len(authValues) == 0 {
			return status.Error(codes.Unauthenticated, "missing authorization header")
		}

		authHeader := authValues[0]

		// Check for Basic auth scheme
		if !strings.HasPrefix(authHeader, "Basic ") {
			return status.Error(codes.Unauthenticated, "invalid authorization scheme")
		}

		// Extract and decode Base64 credentials
		encodedCredentials := strings.TrimPrefix(authHeader, "Basic ")
		decodedBytes, err := base64.StdEncoding.DecodeString(encodedCredentials)
		if err != nil {
			return status.Error(codes.Unauthenticated, "invalid authorization encoding")
		}

		// Parse username:password
		credentials := string(decodedBytes)
		parts := strings.SplitN(credentials, ":", 2)
		if len(parts) != 2 {
			return status.Error(codes.Unauthenticated, "invalid credentials format")
		}

		providedUsername := parts[0]
		providedPassword := parts[1]

		// Validate credentials
		if providedUsername != username || providedPassword != password {
			return status.Error(codes.Unauthenticated, "invalid credentials")
		}

		// Credentials are valid, call the handler
		return handler(srv, ss)
	}
}
