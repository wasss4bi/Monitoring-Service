package config

import (
	"monitoring_service/internal/fetcherclient"
)

func NewFetcherClient() (*fetcherclient.Client, error) {
	tlsConfig, err := fetcherclient.NewTLSConfig(getenv("FETCHER_CA_FILE", ""), getenv("FETCHER_CLIENT_CERT_FILE", ""), getenv("FETCHER_CLIENT_KEY_FILE", ""), getenv("FETCHER_SERVER_NAME", "fetcher"))
	if err != nil {
		return nil, err
	}
	return fetcherclient.New(getenv("FETCHER_ADDRESS", "fetcher:8443"), tlsConfig)
}
