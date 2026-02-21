package main

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
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

// CreateInvoiceRecord is our implementation for the Chi interface.
func (s *ApiServer) CreateInvoiceRecord(w http.ResponseWriter, r *http.Request) {
	// The generated code will handle validation. We just need to decode the body.
	var req api.CreateInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("--> Submitting Transaction: CreateInvoiceRecord, ID: %s", req.RecordId)

	// Use the Fabric SDK to submit the transaction
	_, err := s.Contract.SubmitTransaction("CreateInvoiceRecord", req.RecordId, req.Filename, req.XmlBase64)
	if err != nil {
		// In production, parse the error for specific Fabric error types
		http.Error(w, "Failed to submit transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("<-- Transaction Committed: CreateInvoiceRecord, ID: %s", req.RecordId)

	// Manually set the header and encode the JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":  "Invoice record created",
		"recordId": req.RecordId,
	})
}

// Implement other interface methods here...
func (s *ApiServer) CheckHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
}

// GetRecord demonstrates reading a path parameter.
func (s *ApiServer) GetRecord(w http.ResponseWriter, r *http.Request, recordId string) {
	log.Printf("--> Evaluating Transaction: GetRecord, ID: %s", recordId)

	// Use EvaluateTransaction for read-only queries
	result, err := s.Contract.EvaluateTransaction("GetRecord", recordId)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to evaluate transaction: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("<-- Transaction Evaluated: GetRecord, ID: %s", recordId)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(result) // The result from chaincode is already JSON
}

// CreateBusinessDataRecord implements the POST /records/business-data endpoint
// CreateBusinessDataRecord implements the POST /records/business-data endpoint
func (s *ApiServer) CreateBusinessDataRecord(w http.ResponseWriter, r *http.Request) {
	// 1. Decode the incoming request, which should only contain business data.
	var businessData any
	if err := json.NewDecoder(r.Body).Decode(&businessData); err != nil {
		log.Printf("❌ Error decoding JSON: %v", err)
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 2. Marshal the business data back into a JSON string for the chaincode.
	businessDataBytes, err := json.Marshal(businessData)
	if err != nil {
		// This should rarely happen if the initial decoding worked, but it's good practice.
		log.Printf("❌ Error re-marshaling business data: %v", err)
		http.Error(w, "Invalid businessData format in request: "+err.Error(), http.StatusBadRequest)
		return
	}
	businessDataString := string(businessDataBytes)

	// --- END: MODIFICATION ---

	log.Printf("--> Submitting Transaction: CreateBusinessDataRecord")

	// 3. Submit the transaction with the single, correct argument.
	// The result will be the JSON bytes of the LedgerRecord created by the chaincode.
	resultBytes, err := s.Contract.SubmitTransaction(
		"CreateBusinessDataRecord",
		businessDataString, // Pass the guaranteed JSON string
	)

	// 3. IMPROVED: Handle specific chaincode errors
	if err != nil {
		log.Printf("❌ Failed to submit transaction: %v", err)
		// Check the error message from the chaincode to return a better HTTP status
		if strings.Contains(err.Error(), "already exists") {
			http.Error(w, err.Error(), http.StatusConflict) // 409 Conflict
		} else if strings.Contains(err.Error(), "authorization failed") {
			http.Error(w, err.Error(), http.StatusForbidden) // 403 Forbidden
		} else {
			// For all other errors, return a generic server error
			http.Error(w, "Failed to submit transaction: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	// 4. Unmarshal the response from the chaincode to get the new record.
	var newRecord api.LedgerRecord // Assuming you have this struct defined in your API package
	if err := json.Unmarshal(resultBytes, &newRecord); err != nil {
		log.Printf("❌ Error unmarshaling chaincode response: %v", err)
		http.Error(w, "Failed to parse chaincode response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("<-- Transaction Committed: CreateBusinessDataRecord, RecordID: %s", newRecord.RecordId)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	response := map[string]string{
		"message":  "Business data record created successfully",
		"recordId": newRecord.RecordId,
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("❌ Error encoding response: %v", err)
	}
}

func (s *ApiServer) UpdateBusinessData(w http.ResponseWriter, r *http.Request, recordId string) { /* ... */
}
func (s *ApiServer) GetRecordHistory(w http.ResponseWriter, r *http.Request, recordId string) { /* ... */
}
func (s *ApiServer) UpdateInvoiceRecord(w http.ResponseWriter, r *http.Request, recordId string) { /* ... */
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
	router.Mount("/", handler)

	log.Println("Go (Chi) REST Server listening on port 8080")
	http.ListenAndServe(":8080", router)
}

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
