package main

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"thika-client/api"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/hyperledger/fabric-gateway/pkg/client"
	"github.com/hyperledger/fabric-gateway/pkg/identity"
	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	// Import your generated API package
)

//Api key

const ServerAPIKey = "my-super-secret-and-long-api-key-12345"

// ApiKeyAuthMiddleware checks for a valid API key in the 'X-API-KEY' header.
func ApiKeyAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Some endpoints, like /health or /docs, might not need authentication.
		// We can create a list of public paths.
		publicPaths := []string{"/health", "/docs", "/openapi.json"}
		if slices.Contains(publicPaths, r.URL.Path) {
			next.ServeHTTP(w, r) // It's a public path, so skip the check.
			return
		}

		// Get the key from the request header.
		providedKey := r.Header.Get("X-API-KEY")

		// Check if the key is missing.
		if providedKey == "" {
			log.Println("🚫 API Key missing")
			http.Error(w, "Unauthorized: API Key is missing", http.StatusUnauthorized)
			return
		}

		// Validate the key.
		// NOTE: In a real app, you would use a constant-time comparison function
		// to prevent timing attacks, but this is fine for now.
		if providedKey != ServerAPIKey {
			log.Println("🚫 Invalid API Key provided")
			http.Error(w, "Unauthorized: Invalid API Key", http.StatusUnauthorized)
			return
		}

		log.Println("✅ API Key validated successfully")
		// If the key is valid, call the next handler in the chain.
		next.ServeHTTP(w, r)
	})
}

// ApiServer holds the Fabric contract and implements the generated ServerInterface.
type ApiServer struct {
	Contract *client.Contract
}

type Config struct {
	MspID         string
	CryptoPath    string
	CertPath      string
	KeyPath       string
	TlsCertPath   string
	PeerEndpoint  string
	GatewayPeer   string
	ChannelName   string
	ChaincodeName string
}

// LoadConfig reads configuration from .env file and environment variables
func LoadConfig() (*Config, error) {
	// Load .env file if it exists
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from environment")
	}

	config := &Config{
		MspID:         os.Getenv("FABRIC_MSP_ID"),
		CryptoPath:    os.Getenv("FABRIC_CRYPTO_PATH"),
		CertPath:      os.Getenv("FABRIC_CERT_PATH"),
		KeyPath:       os.Getenv("FABRIC_KEY_PATH"),
		TlsCertPath:   os.Getenv("FABRIC_TLS_CERT_PATH"),
		PeerEndpoint:  os.Getenv("FABRIC_PEER_ENDPOINT"),
		GatewayPeer:   os.Getenv("FABRIC_GATEWAY_PEER"),
		ChannelName:   os.Getenv("FABRIC_CHANNEL_NAME"),
		ChaincodeName: os.Getenv("FABRIC_CHAINCODE_NAME"),
	}

	// Simple validation
	if config.MspID == "" || config.PeerEndpoint == "" {
		return nil, fmt.Errorf("a required environment variable was not set (e.g., FABRIC_MSP_ID)")
	}

	return config, nil
}

func main() {

	// Load configuration from .env file or environment
	config, err := LoadConfig()
	if err != nil {
		log.Fatalf("❌ Could not load config: %v", err)
	}
	// --- Fabric Connection Setup (from previous answers) ---
	gateway, err := connectToFabricGateway(config) // Assume this function returns a connected gateway
	if err != nil {
		log.Fatalf("❌ Could not connect to Fabric Gateway: %v", err)
	}
	defer gateway.Close()
	network := gateway.GetNetwork(config.ChannelName)
	contract := network.GetContract(config.ChaincodeName)
	// ----------------------------------------------------

	// Create an instance of our server implementation
	serverImplementation := &ApiServer{
		Contract: contract,
	}

	// The generated code provides a handler that wraps our implementation.
	// This handler is a standard http.Handler.
	handler := api.Handler(serverImplementation)

	// Create a new Chi router and mount the generated handler.
	router := chi.NewRouter()
	router.Use(middleware.Logger)
	router.Use(middleware.Recoverer)
	router.Use(ApiKeyAuthMiddleware)
	router.Mount("/", handler)

	router.Get("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		swagger, err := api.GetSwagger()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Disable authentication for the spec itself so the UI can load it
		swagger.Servers = nil

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(swagger)
	})

	// 2. Serve Swagger UI
	// This serves a simple HTML page that loads the Swagger UI assets from a CDN.
	router.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(swaggerUIHTML))
	})

	log.Println("Go (Chi) REST Server listening on port 8080")
	log.Println("✅ Swagger UI available at http://localhost:8080/docs")
	http.ListenAndServe(":8080", router)
}

// This HTML loads the Swagger UI scripts from a CDN and points them to your /openapi.json
const swaggerUIHTML = `
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>API Documentation</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui.css" />
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.11.0/swagger-ui-bundle.js" crossorigin></script>
<script>
  window.onload = () => {
    window.ui = SwaggerUIBundle({
      url: '/openapi.json', // Points to the JSON handler we defined above
      dom_id: '#swagger-ui',
    });
  };
</script>
</body>
</html>
`

// =================================================================================
// FABRIC CONNECTION HELPER (NOW USES CONFIG)
// =================================================================================

func connectToFabricGateway(config *Config) (*client.Gateway, error) {
	log.Println("============ Connecting to Fabric Gateway ============")
	clientConnection := newGrpcConnection(config)
	id := newIdentity(config)
	sign := newSigner()

	gateway, err := client.Connect(
		id,
		client.WithSign(sign),
		client.WithClientConnection(clientConnection),
		client.WithEvaluateTimeout(5*time.Second),
		client.WithEndorseTimeout(15*time.Second),
		client.WithSubmitTimeout(5*time.Second),
		client.WithCommitStatusTimeout(1*time.Minute),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gateway: %w", err)
	}
	log.Println("✅ --- Connected to Fabric Gateway ---")
	return gateway, nil
}

func newGrpcConnection(config *Config) *grpc.ClientConn {
	certificate, err := loadCertificate(config.TlsCertPath)
	if err != nil {
		panic(err)
	}
	certPool := x509.NewCertPool()
	certPool.AddCert(certificate)
	transportCredentials := credentials.NewClientTLSFromCert(certPool, config.GatewayPeer)
	connection, err := grpc.NewClient(config.PeerEndpoint, grpc.WithTransportCredentials(transportCredentials))
	if err != nil {
		panic(fmt.Errorf("failed to create gRPC connection: %w", err))
	}
	return connection
}

func newIdentity(config *Config) *identity.X509Identity {
	certificate, err := loadCertificate(config.CertPath)
	if err != nil {
		panic(err)
	}
	id, err := identity.NewX509Identity(config.MspID, certificate)
	if err != nil {
		panic(err)
	}
	return id
}

func newSigner() identity.Sign {
	privateKeyPath := os.Getenv("FABRIC_KEY_PATH")

	// 1. Read the private key file
	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		panic(fmt.Errorf("failed to read private key file: %w", err))
	}

	// 2. Parse the key
	privateKey, err := identity.PrivateKeyFromPEM(privateKeyPEM)
	if err != nil {
		panic(fmt.Errorf("failed to parse private key: %w", err))
	}

	// 3. Create the Sign function
	// NewPrivateKeySign returns a function of type identity.Sign
	sign, err := identity.NewPrivateKeySign(privateKey)
	if err != nil {
		panic(fmt.Errorf("failed to create private key signer: %w", err))
	}

	return sign
}

func loadCertificate(filename string) (*x509.Certificate, error) {
	certificatePEM, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate file: %w", err)
	}
	return identity.CertificateFromPEM(certificatePEM)
}
