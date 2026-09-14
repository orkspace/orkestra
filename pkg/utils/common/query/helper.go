package query

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (q *runtimeQuery) query(url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(q.ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building runtime request: %w", err)
	}
	resp, err := q.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying runtime: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime returned %d", resp.StatusCode)
	}

	return resp, nil
}

func (q *runtimeQuery) result(body interface{}, caller, url string) (interface{}, error) {
	resp, err := q.query(url)
	if err != nil {
		return nil, err
	}

	result := body
	if result == nil {
		result = map[string]interface{}{}
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding %s response: %w", caller, err)
	}

	return result, nil
}
