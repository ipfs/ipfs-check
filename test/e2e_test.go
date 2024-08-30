package test

import (
	"fmt"
	"os"
	"testing"
	"time"
)

const (
	EMPTY_DIR_CID = "QmUNLLsPACCz1vLxQVkXqqLX5R1X345qqfHbsf67hvA3Nn"
)

var (
	BOOTSTRAP_PEER_ADDR string
	WIKIPEDIA_CID       string
	WIKIPEDIA_PEER_ID   string
	WIKIPEDIA_PEER_ADDR string
	SKIP_E2E_TESTS      bool
)

func init() {
	if os.Getenv("E2E_TESTS") == "true" {
		SKIP_E2E_TESTS = false
	} else {
		SKIP_E2E_TESTS = true
		return
	}
	BOOTSTRAP_PEER_ADDR = call("bash", "-c", "ipfs bootstrap list | head -n 1")
	// ipfs name resolve /ipns/en.wikipedia-on-ipfs.org => /ipfs/CID, we remove the /ipfs/ prefix
	WIKIPEDIA_CID = call("ipfs", "name", "resolve", "/ipns/en.wikipedia-on-ipfs.org")[6:]
	WIKIPEDIA_PEER_ID = call("bash", "-c", fmt.Sprintf("ipfs routing findprovs %s | tail -n 1", WIKIPEDIA_CID))
	WIKIPEDIA_PEER_ADDR = fmt.Sprintf("/p2p/%s", WIKIPEDIA_PEER_ID)
}

func TestEmptyDirOnBoostrapPeer(t *testing.T) {
	if SKIP_E2E_TESTS {
		t.Skip("Skipping e2e tests")
	}
	obj := Q(t, EMPTY_DIR_CID, BOOTSTRAP_PEER_ADDR)

	obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsTrue()
	obj.Value("ConnectionError").String().IsEmpty()
	obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
	obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
	obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
}

func TestWikipediaOnSomeProviderPeer(t *testing.T) {
	if SKIP_E2E_TESTS {
		t.Skip("Skipping e2e tests")
	}
	obj := Q(t, WIKIPEDIA_CID, WIKIPEDIA_PEER_ADDR)
	obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsTrue()
	// It seems that most peers do not provide over bitswap:
	// obj.Value("ConnectionError").String().IsEmpty()
	// obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
	// obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
	// obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
}

func TestRandomFileOnBootstrapPeer(t *testing.T) {
	if SKIP_E2E_TESTS {
		t.Skip("Skipping e2e tests")
	}
	t.Skip("the random file CID is marked as \"not found in the DHT\" when calling bootstrap peers")

	randomFileCid := call("bash", "-c", "cat /dev/urandom | head | sha256sum | ipfs add --quiet -")

	callWhile(
		func() {
			time.Sleep(60 * time.Second)
			obj := Q(t, randomFileCid, BOOTSTRAP_PEER_ADDR)

			obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsTrue()
			obj.Value("ConnectionError").String().IsEmpty()
			obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
			obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
			obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
		},
		"ipfs", "dht", "provide", randomFileCid, "--verbose")
}

func TestRandomFileOnLocalPeer(t *testing.T) {
	if SKIP_E2E_TESTS {
		t.Skip("Skipping e2e tests")
	}
	// ipfs id -f "<id>"
	nodeId := call("ipfs", "id", "-f", "<id>")
	localAddr := fmt.Sprintf("/p2p/%s", nodeId)

	// cat /dev/urandom | head | ipfs add --quiet -
	randomFileCid := call("bash", "-c", "cat /dev/urandom | head | sha256sum | ipfs add --quiet")

	callWhile(
		func() {
			time.Sleep(25 * time.Second)
			obj := Q(t, randomFileCid, localAddr)

			obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsTrue()
			obj.Value("ConnectionError").String().IsEmpty()
			obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
			obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
			obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
		},
		"ipfs", "dht", "provide", randomFileCid,
	)
}

func TestRandomFileNeverUploadedOnBootstrapPeer(t *testing.T) {
	if SKIP_E2E_TESTS {
		t.Skip("Skipping e2e tests")
	}
	randomFileCid := call("bash", "-c", "cat /dev/urandom | head | sha256sum | ipfs add --quiet --only-hash -")

	obj := Q(t, randomFileCid, BOOTSTRAP_PEER_ADDR)

	obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsFalse()
	obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsFalse()
	obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
}
