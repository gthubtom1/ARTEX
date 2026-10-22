package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 数据定位接口契约：前端读 tables/proof_level/notes（snake_case）。
func TestDataLocateContract(t *testing.T) {
	s := &Server{}
	mux := http.NewServeMux()
	s.registerDataRoutes(mux)

	body := `{"engine":"mysql","output":"+------------+------------+\n| table_name | table_rows |\n+------------+------------+\n| users      |      12430 |\n| orders     |       8810 |\n+------------+------------+"}`
	req := httptest.NewRequest("POST", "/api/data/locate", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Engine string `json:"engine"`
		Result struct {
			Tables     []map[string]any `json:"tables"`
			ProofLevel string           `json:"proof_level"`
			Notes      []string         `json:"notes"`
		} `json:"result"`
		Probe []string `json:"probe"`
		Note  string   `json:"note"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tables) != 2 {
		t.Fatalf("tables: %+v (%s)", resp.Result.Tables, w.Body.String())
	}
	if resp.Result.ProofLevel == "" || resp.Result.ProofLevel == "D0" {
		t.Fatalf("proof level: %s", resp.Result.ProofLevel)
	}
	if len(resp.Probe) == 0 || resp.Note == "" {
		t.Fatalf("probe/note missing: %+v", resp)
	}
	if _, ok := resp.Result.Tables[0]["name"]; !ok {
		t.Fatalf("snake_case contract broken: %+v", resp.Result.Tables[0])
	}
}
