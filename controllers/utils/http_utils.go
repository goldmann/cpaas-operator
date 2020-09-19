package utils

import (
	"io/ioutil"
	"net/http"
	"time"

	"github.com/go-logr/logr"
)

// HTTPUtil is used to interact with remote servers over HTTP
type HTTPUtil struct {
	Log logr.Logger
}

// Get reads file from a remote host over HTTP.
func (util *HTTPUtil) Get(url string, timeout time.Duration) (string, error) {
	util.Log.Info("Reading url", "Http.Url", url)

	client := http.Client{
		Timeout: timeout,
	}

	resp, err := client.Get(url)

	if err != nil {
		util.Log.Error(err, "An error occurred while reading remote url", "Http.Url", url)
		return "", err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)

	if err != nil {
		util.Log.Error(err, "An error occurred while reading body", "Http.Url", url)
		return "", err
	}

	return string(body), nil
}
