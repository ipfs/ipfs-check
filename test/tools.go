package test

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/gavv/httpexpect/v2"
)

func Q(
	t *testing.T,
	cid string,
	multiaddr string,
) *httpexpect.Object {
	url := GetEnv("GATEWAY_URL", "http://localhost:3333")
	// url := GetEnv("GATEWAY_URL", "https://ipfs-check-backend.ipfs.io")
	return Query(t, url, cid, multiaddr)
}

func Query(
	t *testing.T,
	url string,
	cid string,
	multiaddr string,
) *httpexpect.Object {
	expectedContentType := "application/json"
	if url == "https://ipfs-check-backend.ipfs.io" {
		// Temporary patch: the current released gateway returns text/plain.
		// TODO: when the correct Content-Type is released, remove all code related to this
		// override.
		expectedContentType = "text/plain"
	}

	opts := httpexpect.ContentOpts{
		MediaType: expectedContentType,
	}

	e := httpexpect.Default(t, url)

	return e.POST("/").
		WithQuery("cid", cid).
		WithQuery("multiaddr", multiaddr).
		Expect().
		Status(http.StatusOK).
		JSON(opts).Object()
}

func GetEnv(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

/**
Example of outputs:
```json
{
	"CidInDHT": true,
	"ConnectionError": "no addresses",
	"DataAvailableOverBitswap": {
		"Duration": 0,
		"Error": "could not connect to peer",
		"Found": false,
		"Responded": false
	},
	"PeerFoundInDHT": {}
}
```
*/

func call(name string, xs ...string) string {
	cmd := exec.Command(name, xs...)
	output, err := cmd.Output()

	if err != nil {
		panic(err)
	}

	result := string(output)
	return strings.TrimSpace(result)
}

func callWhile(fct func(), name string, xs ...string) {
	var output bytes.Buffer

	cmd := exec.Command(name, xs...)
	cmd.Stdout = &output
	err := cmd.Start()

	if err != nil {
		panic(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	defer func() {
		select {
		case err := <-done:
			if err != nil {
				panic(err)
			} else {
				fmt.Printf("Command exited successfully. Output:\n%s\n", output.String())
			}
		default:
			// The command is still running, so we just send the SIGTERM signal
			cmd.Process.Signal(syscall.SIGTERM)
			fmt.Printf("Command killed. Output:\n%s\n", output.String())
		}
	}()

	fct()
}
