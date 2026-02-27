package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"thika-client/api"
	"time"
)

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

func (s *ApiServer) UpdateBusinessData(w http.ResponseWriter, r *http.Request, recordId string) {
	// 1. Decode the incoming request body
	var req api.UpdateBusinessDataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("❌ Error decoding JSON: %v", err)
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 2. Marshal the business data object into a JSON string
	// The chaincode expects the second argument to be a string representation of the JSON.
	businessDataBytes, err := json.Marshal(req.BusinessData)
	if err != nil {
		log.Printf("❌ Error marshaling business data: %v", err)
		http.Error(w, "Invalid businessData format: "+err.Error(), http.StatusBadRequest)
		return
	}
	businessDataString := string(businessDataBytes)

	log.Printf("--> Submitting Transaction: UpdateBusinessData, ID: %s", recordId)

	// 3. Submit the transaction to the ledger
	// Chaincode signature: UpdateBusinessData(ctx, recordID, newBusinessDataString)
	resultBytes, err := s.Contract.SubmitTransaction(
		"UpdateBusinessData",
		recordId,
		businessDataString,
	)

	// 4. Handle Chaincode Errors
	if err != nil {
		log.Printf("❌ Failed to submit transaction: %v", err)
		errMsg := err.Error()

		if strings.Contains(errMsg, "does not exist") {
			http.Error(w, "Record not found: "+errMsg, http.StatusNotFound)
		} else if strings.Contains(errMsg, "access denied") || strings.Contains(errMsg, "authorization failed") {
			http.Error(w, "Permission denied: "+errMsg, http.StatusForbidden)
		} else if strings.Contains(errMsg, "locked") {
			// If the record is locked by a policy, return 423 Locked (WebDAV) or 403 Forbidden
			http.Error(w, "Record is locked and cannot be modified: "+errMsg, http.StatusForbidden)
		} else {
			http.Error(w, "Failed to submit transaction: "+errMsg, http.StatusInternalServerError)
		}
		return
	}

	// 5. Parse the result (The updated LedgerRecord)
	var updatedRecord api.LedgerRecord
	if err := json.Unmarshal(resultBytes, &updatedRecord); err != nil {
		log.Printf("❌ Error unmarshaling chaincode response: %v", err)
		http.Error(w, "Failed to parse chaincode response", http.StatusInternalServerError)
		return
	}

	log.Printf("<-- Transaction Committed: UpdateBusinessData, ID: %s", recordId)

	// 6. Send the response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(updatedRecord); err != nil {
		log.Printf("❌ Error encoding response: %v", err)
	}
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

func (s *ApiServer) CheckHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
}

// GetRecord retrieves a record and optionally its history.
// This replaces the old GetRecordHistory handler.
func (s *ApiServer) GetRecord(w http.ResponseWriter, r *http.Request, recordId string, params api.GetRecordParams) {

	// 1. Fetch the Current State (Always required)
	// We use the "ReadRecord" transaction from the chaincode.
	recordBytes, err := s.Contract.EvaluateTransaction("ReadRecord", recordId)
	if err != nil {
		// Handle "not found" vs "internal error"
		if strings.Contains(err.Error(), "does not exist") {
			http.Error(w, "Record not found", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to fetch record: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Unmarshal the record data
	var record api.LedgerRecord
	if err := json.Unmarshal(recordBytes, &record); err != nil {
		http.Error(w, "Failed to parse record data", http.StatusInternalServerError)
		return
	}

	// 2. Prepare the Response Wrapper
	// We use the new RecordResponse struct defined in your YAML.
	response := api.RecordResponse{
		Data:    record,
		History: nil, // Default to nil
	}

	// 3. Check if History is requested
	// The params.IncludeHistory field is a pointer (*bool), so we check if it's not nil and true.
	if params.IncludeHistory != nil && *params.IncludeHistory {
		// Fetch History using the existing chaincode function
		// We pass empty strings "" for start/end to get the full history
		historyBytes, err := s.Contract.EvaluateTransaction("GetRecordHistoryByID", recordId, "", "")
		if err != nil {
			// Log the warning but return the record data we already found.
			// Alternatively, you could return a 500 error here if history is critical.
			log.Printf("⚠️ Failed to fetch history for %s: %v", recordId, err)
		} else {
			var history []api.HistoryEntry
			if err := json.Unmarshal(historyBytes, &history); err == nil {
				response.History = &history
			}
		}
	}

	// 4. Return the Composite Response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("❌ Error encoding response: %v", err)
	}
}

// GetRecordsByDateRange handles the API request to search for records within a date range.
func (s *ApiServer) GetRecordsByDateRange(w http.ResponseWriter, r *http.Request, params api.GetRecordsByDateRangeParams) {
	// 1. Prepare Chaincode Arguments from API Parameters
	// The 'params' struct is automatically populated by the generated Chi middleware.

	// Format time.Time objects back to RFC3339 strings for the chaincode.
	startStr := params.Start.Format(time.RFC3339)
	endStr := params.End.Format(time.RFC3339)

	// Handle optional pageSize with a default value.
	var pageSize int32 = 10 // Default page size
	if params.PageSize != nil {
		pageSize = int32(*params.PageSize)
	}
	pageSizeStr := strconv.Itoa(int(pageSize))

	// Handle optional bookmark.
	var bookmark string
	if params.Bookmark != nil {
		bookmark = *params.Bookmark
	}

	log.Printf("--> Evaluating Transaction: GetRecordsByDateRange, Start: %s, End: %s, PageSize: %s", startStr, endStr, pageSizeStr)

	// 2. Evaluate the transaction (read-only query)
	// Chaincode signature: GetRecordsByDateRange(ctx, startStr, endStr, pageSize, bookmark)
	resultBytes, err := s.Contract.EvaluateTransaction(
		"GetRecordsByDateRange",
		startStr,
		endStr,
		pageSizeStr,
		bookmark,
	)

	// 3. Handle Chaincode Errors
	if err != nil {
		log.Printf("❌ Failed to evaluate transaction: %v", err)
		errMsg := err.Error()

		// Map specific chaincode errors to user-friendly HTTP status codes.
		if strings.Contains(errMsg, "invalid time format") {
			http.Error(w, "Bad Request: "+errMsg, http.StatusBadRequest)
		} else {
			http.Error(w, "Internal Server Error: "+errMsg, http.StatusInternalServerError)
		}
		return
	}

	// 4. Unmarshal the response from the chaincode
	// The chaincode returns a PaginatedResponse struct, which we unmarshal.
	var paginatedResponse api.PaginatedResponse
	if err := json.Unmarshal(resultBytes, &paginatedResponse); err != nil {
		log.Printf("❌ Error unmarshaling chaincode response: %v", err)
		http.Error(w, "Failed to parse chaincode response", http.StatusInternalServerError)
		return
	}

	log.Printf("<-- Transaction Evaluated: Found %d records", paginatedResponse.RecordsCount)

	// 5. Send the successful response to the client
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(paginatedResponse); err != nil {
		// This error happens if the connection is closed, so we just log it.
		log.Printf("❌ Error encoding response: %v", err)
	}
}

func (s *ApiServer) UpdateInvoiceRecord(w http.ResponseWriter, r *http.Request, recordId string) { /* ... */
}
