package main

import (
	"encoding/base64"
	"encoding/json"
)

type configData struct {
	Addr string `json:"a"`
	Pass string `json:"p"`
	SNI  string `json:"s"`
}

func generateConfigString(addr, pass, sni string) string {
	data := configData{Addr: addr, Pass: pass, SNI: sni}
	b, _ := json.Marshal(data)
	return "gt://" + base64.StdEncoding.EncodeToString(b)
}

func parseConfigString(raw string) (configData, error) {
	if len(raw) > 5 && raw[:5] == "gt://" {
		raw = raw[5:]
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return configData{}, err
	}
	var data configData
	if err := json.Unmarshal(b, &data); err != nil {
		return configData{}, err
	}
	return data, nil
}
