package grpc

import (
	"context"
	"encoding/base64"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// MockServerStream is a mock gRPC server stream for testing
type MockServerStream struct {
	ctx context.Context
}

func (m *MockServerStream) Context() context.Context {
	return m.ctx
}

func (m *MockServerStream) SendHeader(metadata.MD) error {
	return nil
}

func (m *MockServerStream) SetHeader(metadata.MD) error {
	return nil
}

func (m *MockServerStream) SetTrailer(metadata.MD) {}

func (m *MockServerStream) SendMsg(m2 interface{}) error {
	return nil
}

func (m *MockServerStream) RecvMsg(m2 interface{}) error {
	return nil
}

func TestStreamAuthInterceptorValidCredentials(t *testing.T) {
	username := "testuser"
	password := "testpass"
	interceptor := NewStreamAuthInterceptor(username, password)

	// Create valid Basic auth header
	credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	md := metadata.Pairs("authorization", "Basic "+credentials)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockServerStream{ctx: ctx}
	handlerCalled := false

	handler := func(srv interface{}, ss grpc.ServerStream) error {
		handlerCalled = true
		return nil
	}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, handler)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if !handlerCalled {
		t.Fatal("Handler was not called")
	}
}

func TestStreamAuthInterceptorInvalidPassword(t *testing.T) {
	username := "testuser"
	password := "correctpass"
	interceptor := NewStreamAuthInterceptor(username, password)

	// Create Basic auth header with wrong password
	credentials := base64.StdEncoding.EncodeToString([]byte(username + ":wrongpass"))
	md := metadata.Pairs("authorization", "Basic "+credentials)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockServerStream{ctx: ctx}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, func(srv interface{}, ss grpc.ServerStream) error {
		t.Fatal("Handler should not be called")
		return nil
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.Unauthenticated {
		t.Fatalf("Expected Unauthenticated, got %v", st.Code())
	}
}

func TestStreamAuthInterceptorMissingHeader(t *testing.T) {
	interceptor := NewStreamAuthInterceptor("user", "pass")

	// No authorization header
	md := metadata.Pairs()
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockServerStream{ctx: ctx}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, func(srv interface{}, ss grpc.ServerStream) error {
		t.Fatal("Handler should not be called")
		return nil
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.Unauthenticated {
		t.Fatalf("Expected Unauthenticated, got %v", st.Code())
	}
}

func TestStreamAuthInterceptorWrongUsername(t *testing.T) {
	username := "testuser"
	password := "testpass"
	interceptor := NewStreamAuthInterceptor(username, password)

	// Create Basic auth header with wrong username
	credentials := base64.StdEncoding.EncodeToString([]byte("wronguser:" + password))
	md := metadata.Pairs("authorization", "Basic "+credentials)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockServerStream{ctx: ctx}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, func(srv interface{}, ss grpc.ServerStream) error {
		t.Fatal("Handler should not be called")
		return nil
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.Unauthenticated {
		t.Fatalf("Expected Unauthenticated, got %v", st.Code())
	}
}

func TestStreamAuthInterceptorInvalidBase64(t *testing.T) {
	interceptor := NewStreamAuthInterceptor("user", "pass")

	// Invalid Base64
	md := metadata.Pairs("authorization", "Basic !!!invalid!!!")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	stream := &MockServerStream{ctx: ctx}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, func(srv interface{}, ss grpc.ServerStream) error {
		t.Fatal("Handler should not be called")
		return nil
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.Unauthenticated {
		t.Fatalf("Expected Unauthenticated, got %v", st.Code())
	}
}

func TestStreamAuthInterceptorNoMetadata(t *testing.T) {
	interceptor := NewStreamAuthInterceptor("user", "pass")

	// No metadata at all
	ctx := context.Background()
	stream := &MockServerStream{ctx: ctx}

	err := interceptor(nil, stream, &grpc.StreamServerInfo{}, func(srv interface{}, ss grpc.ServerStream) error {
		t.Fatal("Handler should not be called")
		return nil
	})

	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got %T", err)
	}

	if st.Code() != codes.Unauthenticated {
		t.Fatalf("Expected Unauthenticated, got %v", st.Code())
	}
}
