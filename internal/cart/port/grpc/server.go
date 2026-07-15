package grpc

import (
	"google.golang.org/grpc"

	"goshop/internal/cart/service"
	pb "goshop/proto/gen/go/cart"
)

func RegisterHandlers(svr *grpc.Server, cartService service.ICartService) {
	pb.RegisterCartServiceServer(svr, NewCartHandler(cartService))
}
