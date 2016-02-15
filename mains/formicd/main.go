package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"

	pb "github.com/creiht/formic/proto"

	"net"
)

var (
	usetls             = flag.Bool("tls", true, "Connection uses TLS if true, else plain TCP")
	certFile           = flag.String("cert_file", "/etc/oort/server.crt", "The TLS cert file")
	keyFile            = flag.String("key_file", "/etc/oort/server.key", "The TLS key file")
	port               = flag.Int("port", 9443, "The server port")
	oortValueHost      = flag.String("oortvaluehost", "127.0.0.1:6379", "host:port to use when connecting to oort value")
	oortGroupHost      = flag.String("oortgrouphost", "127.0.0.1:6380", "host:port to use when connecting to oort group")
	insecureSkipVerify = flag.Bool("skipverify", true, "don't verify cert")
)

// FatalIf is just a lazy log/panic on error func
func FatalIf(err error, msg string) {
	if err != nil {
		grpclog.Fatalf("%s: %v", msg, err)
	}
}

func main() {
	flag.Parse()

	envtls := os.Getenv("FORMICD_TLS")
	if envtls == "true" {
		*usetls = true
	}

	envoortvhost := os.Getenv("FORMICD_OORT_VALUE_HOST")
	if envoortvhost != "" {
		*oortValueHost = envoortvhost
	}

	envoortghost := os.Getenv("FORMICD_OORT_GROUP_HOST")
	if envoortghost != "" {
		*oortGroupHost = envoortghost
	}

	envport := os.Getenv("FORMICD_PORT")
	if envport != "" {
		p, err := strconv.Atoi(envport)
		if err != nil {
			log.Println("Did not send valid port from env:", err)
		} else {
			*port = p
		}
	}

	envcert := os.Getenv("FORMICD_CERT_FILE")
	if envcert != "" {
		*certFile = envcert
	}

	envkey := os.Getenv("FORMICD_KEY_FILE")
	if envkey != "" {
		*keyFile = envkey
	}

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	FatalIf(err, "Failed to bind to port")

	var opts []grpc.ServerOption
	if *usetls {
		creds, err := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		FatalIf(err, "Couldn't load cert from file")
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	s := grpc.NewServer(opts...)
	fs, err := NewOortFS(*oortValueHost, *oortGroupHost, *insecureSkipVerify)
	if err != nil {
		grpclog.Fatalln(err)
	}
	pb.RegisterApiServer(s, NewApiServer(fs))
	grpclog.Printf("Starting up on %d...\n", *port)
	s.Serve(l)
}
