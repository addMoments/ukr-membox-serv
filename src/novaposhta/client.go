package novaposhta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const apiURL = "https://api.novaposhta.ua/v2.0/json/"

type npRequest struct {
	APIKey           string         `json:"apiKey"`
	ModelName        string         `json:"modelName"`
	CalledMethod     string         `json:"calledMethod"`
	MethodProperties map[string]any `json:"methodProperties"`
}

type npResponse struct {
	Success bool              `json:"success"`
	Data    []json.RawMessage `json:"data"`
	Errors  []string          `json:"errors"`
}

// call sends a request to the Nova Poshta API and returns the raw data array.
func call(apiKey, modelName, method string, props map[string]any) ([]json.RawMessage, error) {
	reqBody := npRequest{
		APIKey:           apiKey,
		ModelName:        modelName,
		CalledMethod:     method,
		MethodProperties: props,
	}

	byt, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("novaposhta: marshal: %w", err)
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewReader(byt))
	if err != nil {
		return nil, fmt.Errorf("novaposhta: http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("novaposhta: read: %w", err)
	}

	var npResp npResponse
	if err := json.Unmarshal(body, &npResp); err != nil {
		return nil, fmt.Errorf("novaposhta: unmarshal: %w", err)
	}

	if !npResp.Success {
		return nil, fmt.Errorf("novaposhta: api error: %v", npResp.Errors)
	}

	return npResp.Data, nil
}
