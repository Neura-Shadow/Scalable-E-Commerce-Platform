package grpc

import (
	"testing"

	goGRPC "google.golang.org/grpc"

	"goshop/internal/cart/service/mocks"
)

func TestRegisterHandlers(t *testing.T) {
	RegisterHandlers(goGRPC.NewServer(), mocks.NewICartService(t))
}
