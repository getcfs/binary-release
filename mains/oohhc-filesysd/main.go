package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	fb "github.com/letterj/oohhc/proto/filesystem"

	"github.com/pandemicsyn/ftls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/grpclog"

	"net"
)

var (
	usetls             = flag.Bool("tls", true, "Connection uses TLS if true, else plain TCP")
	certFile           = flag.String("cert_file", "/etc/oort/server.crt", "The TLS cert file")
	keyFile            = flag.String("key_file", "/etc/oort/server.key", "The TLS key file")
	port               = flag.Int("port", 8448, "The filesysd server port")
	oortGroupHost      = flag.String("oortgrouphost", "127.0.0.1:6380", "host:port to use when connecting to oort group")
	insecureSkipVerify = flag.Bool("skipverify", true, "don't verify cert")
	// Group Store Values
	mutualtlsGS          = flag.Bool("mutualtlsGS", true, "Turn on MutualTLS for Group Store")
	insecureSkipVerifyGS = flag.Bool("insecureSkipVerifyGS", false, "Don't verify cert for Group Store")
	certFileGS           = flag.String("certfileGS", "/etc/oort/client.crt", "The client TLS cert file for the Group Store")
	keyFileGS            = flag.String("keyFileGS", "/etc/oort/client.key", "The client TLS key file for the Group Store")
	caFileGS             = flag.String("cafileGS", "/etc/oort/ca.pem", "The client CA file")
)

// FatalIf is just a lazy log/panic on error func
func FatalIf(err error, msg string) {
	if err != nil {
		grpclog.Fatalf("%s: %v", msg, err)
	}
}

func main() {
	flag.Parse()

	envMutualtlsGS := os.Getenv("OOHHC_FILESYS_GS_MUTUALTLS")
	if envMutualtlsGS == "false" {
		*mutualtlsGS = false
	}

	envInsecureSkipVerifyGS := os.Getenv("OOHHC_FILESYS_GS_SKIP_VERIFY")
	if envInsecureSkipVerifyGS == "false" {
		*insecureSkipVerify = false
	}

	envCertFileGS := os.Getenv("OOHHC_FILESYS_GS_CERT_FILE")
	if envCertFileGS != "" {
		*certFileGS = envCertFileGS
	}

	envKeyFileGS := os.Getenv("OOHHC_FILESYS_GS_KEY_FILE")
	if envKeyFileGS != "" {
		*keyFileGS = envKeyFileGS
	}

	envCAFileGS := os.Getenv("OOHHC_FILESYS_GS_CA_FILE")
	if envCAFileGS != "" {
		*caFileGS = envCAFileGS
	}

	envtls := os.Getenv("OOHHC_FILESYS_TLS")
	if envtls == "false" {
		*usetls = false
	}

	envoortghost := os.Getenv("OOHHC_FILESYS_OORT_GROUP_HOST")
	if envoortghost != "" {
		*oortGroupHost = envoortghost
	}

	envport := os.Getenv("OOHHC_FILESYS_PORT")
	if envport != "" {
		p, err := strconv.Atoi(envport)
		if err != nil {
			log.Println("Did not send valid port from env:", err)
		} else {
			*port = p
		}
	}

	envcert := os.Getenv("OOHHC_FILESYS_CERT_FILE")
	if envcert != "" {
		*certFile = envcert
	}

	envkey := os.Getenv("OOHHC_FILESYS_KEY_FILE")
	if envkey != "" {
		*keyFile = envkey
	}

	envSkipVerify := os.Getenv("OOHHC_FILESYS_SKIP_VERIFY")
	if envSkipVerify != "true" {
		*insecureSkipVerify = true
	}

	// Setup Group Store connection properties
	gopt, gerr := ftls.NewGRPCClientDialOpt(&ftls.Config{
		MutualTLS:          *mutualtlsGS,
		InsecureSkipVerify: *insecureSkipVerifyGS,
		CertFile:           *certFileGS,
		KeyFile:            *keyFileGS,
		CAFile:             *caFileGS,
	})
	if gerr != nil {
		grpclog.Fatalln("Cannot setup tls config:", gerr)
	}

	// Bind to port
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	FatalIf(err, "Failed to bind to port")

	// Setup up FileSystem Web Service Properties
	var opts []grpc.ServerOption
	if *usetls {
		creds, cerr := credentials.NewServerTLSFromFile(*certFile, *keyFile)
		FatalIf(cerr, "Couldn't load cert from file")
		opts = []grpc.ServerOption{grpc.Creds(creds)}
	}

	// Setup and Start FileSystem Web Service
	s := grpc.NewServer(opts...)
	ws, err := NewFileSystemWS(*oortGroupHost, *insecureSkipVerify, gopt)
	if err != nil {
		grpclog.Fatalln(err)
	}
	fb.RegisterFileSystemAPIServer(s, NewFileSystemAPIServer(ws))
	grpclog.Printf("Starting up on %d...\n", *port)
	s.Serve(lis)
}
