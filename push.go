package firego

import "encoding/json"

// Push creates a reference to an auto-generated child location.
func (fb *Firebase) Push(v interface{}) (*Firebase, error) {
	bytes, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	bytes, err = fb.doRequest("POST", bytes)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if err := json.Unmarshal(bytes, &m); err != nil {
		return nil, err
	}
	return &Firebase{
		repo:   fb.repo,
		path:   fb.path + "/" + m["name"],
		client: fb.client,
	}, err
}
