package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	"github.com/ipfs/go-cid"
	ci "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/stretchr/testify/require"
)

// mockNameSystem implements namesys.NameSystem for testing
type mockNameSystem struct {
	resolveFunc func(context.Context, path.Path) (namesys.Result, error)
}

func (m *mockNameSystem) Resolve(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) (namesys.Result, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, p)
	}
	return namesys.Result{}, errors.New("not implemented")
}

func (m *mockNameSystem) ResolveAsync(ctx context.Context, p path.Path, opts ...namesys.ResolveOption) <-chan namesys.AsyncResult {
	ch := make(chan namesys.AsyncResult, 1)
	close(ch)
	return ch
}

func (m *mockNameSystem) Publish(ctx context.Context, sk ci.PrivKey, value path.Path, opts ...namesys.PublishOption) error {
	return errors.New("not implemented")
}

func TestBuildDiagnosticURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "domain with dot",
			input:    "example.com",
			expected: "https://dnslink.dev/#example.com",
		},
		{
			name:     "subdomain",
			input:    "subdomain.example.com",
			expected: "https://dnslink.dev/#subdomain.example.com",
		},
		{
			name:     "IPNS key without dot",
			input:    "k51qzi5uqu5dlvj2baxnqndepeb86cbk3ng7n3i46uzyxzyqj2xjonzllnv0v8",
			expected: "https://ipns.ipfs.network/#k51qzi5uqu5dlvj2baxnqndepeb86cbk3ng7n3i46uzyxzyqj2xjonzllnv0v8",
		},
		{
			name:     "legacy PeerID",
			input:    "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
			expected: "https://ipns.ipfs.network/#QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildDiagnosticURL(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveInput_RegularCID(t *testing.T) {
	ctx := context.Background()
	mockNS := &mockNameSystem{}

	// Regular content CID should be returned immediately without resolution
	input := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	expectedCID, err := cid.Decode(input)
	require.NoError(t, err)

	resultCID, mutableRes, err := resolveInput(ctx, mockNS, input)
	require.NoError(t, err)
	require.Nil(t, mutableRes)
	require.Equal(t, expectedCID, resultCID)
}

func TestResolveInput_PeerIDv1(t *testing.T) {
	ctx := context.Background()

	// PeerID in CIDv1 format (libp2p-key codec 0x72)
	// Reference: https://github.com/libp2p/specs/blob/master/peer-ids/peer-ids.md#string-representation
	// Example from spec: bafzbeie5745rpv2m6tjyuugywy4d5ewrqgqqhfnf445he3omzpjbx5xqxe
	input := "bafzaajaiaejcbzdibmxyzdjbbehgvizh6g5cjyzhqlidscv64ubjeeeqk4w2nlj2"
	resolvedCID := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			require.True(t, p.Mutable())
			require.Equal(t, "/ipns/"+input, p.String())

			resolvedPath, err := path.NewPath("/ipfs/" + resolvedCID)
			require.NoError(t, err)
			return namesys.Result{Path: resolvedPath}, nil
		},
	}

	resultCID, mutableRes, err := resolveInput(ctx, mockNS, input)
	require.NoError(t, err)
	require.NotNil(t, mutableRes)
	require.True(t, mutableRes.IsMutableInput, "PeerID v1 should be marked as mutable input")
	require.Equal(t, "/ipns/"+input, mutableRes.InputPath)
	require.Equal(t, "/ipfs/"+resolvedCID, mutableRes.ResolvedPath)
	require.Contains(t, mutableRes.DiagnosticURL, "ipns.ipfs.network")

	expectedCID, err := cid.Decode(resolvedCID)
	require.NoError(t, err)
	require.Equal(t, expectedCID, resultCID)
}

func TestResolveInput_LegacyPeerIDAsContentCID(t *testing.T) {
	ctx := context.Background()
	mockNS := &mockNameSystem{}

	// Bare legacy PeerID (base58 multihash starting with Qm) is ambiguous
	// Reference: https://github.com/libp2p/specs/blob/master/peer-ids/peer-ids.md#string-representation
	// Example from spec: QmYyQSo1c1Ym7orWxLYvCrM2EmxFTANf8wXmmE7DWjhx5N
	// UX decision: treat as content CID (immutable) by default, not IPNS name
	input := "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG"
	expectedCID, err := cid.Decode(input)
	require.NoError(t, err)

	resultCID, mutableRes, err := resolveInput(ctx, mockNS, input)
	require.NoError(t, err)
	require.Nil(t, mutableRes, "Bare Qm... should be treated as content CID, not IPNS")
	require.Equal(t, expectedCID, resultCID)
}

func TestResolveInput_ExplicitIPNSPath(t *testing.T) {
	ctx := context.Background()

	// Explicit /ipns/ prefix makes intent clear - resolve as IPNS
	legacyPeerID := "QmYwAPJzv5CZsnA625s3Xf2nemtYgPpHdWEz79ojWnPbdG"
	input := "/ipns/" + legacyPeerID
	resolvedCID := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			require.True(t, p.Mutable())
			require.Equal(t, input, p.String())

			resolvedPath, err := path.NewPath("/ipfs/" + resolvedCID)
			require.NoError(t, err)
			return namesys.Result{Path: resolvedPath}, nil
		},
	}

	resultCID, mutableRes, err := resolveInput(ctx, mockNS, input)
	require.NoError(t, err)
	require.NotNil(t, mutableRes)
	require.True(t, mutableRes.IsMutableInput, "Explicit /ipns/ path should be marked as mutable")
	require.Equal(t, input, mutableRes.InputPath)
	require.Equal(t, "/ipfs/"+resolvedCID, mutableRes.ResolvedPath)
	require.Contains(t, mutableRes.DiagnosticURL, "ipns.ipfs.network")

	expectedCID, err := cid.Decode(resolvedCID)
	require.NoError(t, err)
	require.Equal(t, expectedCID, resultCID)
}

func TestResolveInput_Ed25519PeerID(t *testing.T) {
	ctx := context.Background()

	// Ed25519 identity multihash format from libp2p spec
	// Reference: https://github.com/libp2p/specs/blob/master/peer-ids/peer-ids.md#string-representation
	// Example from spec: 12D3KooWD3eckifWpRn9wQpMG9R9hX3sD158z7EqHWmweQAJU5SA
	// This format does NOT decode as CID, only as PeerID via peer.Decode()
	input := "12D3KooWD3eckifWpRn9wQpMG9R9hX3sD158z7EqHWmweQAJU5SA"
	resolvedCID := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			require.True(t, p.Mutable())
			require.Equal(t, "/ipns/"+input, p.String())

			resolvedPath, err := path.NewPath("/ipfs/" + resolvedCID)
			require.NoError(t, err)
			return namesys.Result{Path: resolvedPath}, nil
		},
	}

	resultCID, mutableRes, err := resolveInput(ctx, mockNS, input)
	require.NoError(t, err)
	require.NotNil(t, mutableRes)
	require.True(t, mutableRes.IsMutableInput, "Ed25519 PeerID should be marked as mutable input")
	require.Equal(t, "/ipns/"+input, mutableRes.InputPath)
	require.Equal(t, "/ipfs/"+resolvedCID, mutableRes.ResolvedPath)
	require.Contains(t, mutableRes.DiagnosticURL, "ipns.ipfs.network")

	expectedCID, err := cid.Decode(resolvedCID)
	require.NoError(t, err)
	require.Equal(t, expectedCID, resultCID)
}

func TestResolveInput_DNSLink(t *testing.T) {
	ctx := context.Background()

	input := "example.com"
	resolvedCID := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			require.True(t, p.Mutable())
			require.Equal(t, "/ipns/"+input, p.String())

			resolvedPath, err := path.NewPath("/ipfs/" + resolvedCID)
			require.NoError(t, err)
			return namesys.Result{Path: resolvedPath}, nil
		},
	}

	resultCID, mutableRes, err := resolveInput(ctx, mockNS, input)
	require.NoError(t, err)
	require.NotNil(t, mutableRes)
	require.True(t, mutableRes.IsMutableInput, "DNSLink should be marked as mutable input")
	require.Equal(t, "/ipns/"+input, mutableRes.InputPath)
	require.Equal(t, "/ipfs/"+resolvedCID, mutableRes.ResolvedPath)
	require.Contains(t, mutableRes.DiagnosticURL, "dnslink.dev")

	expectedCID, err := cid.Decode(resolvedCID)
	require.NoError(t, err)
	require.Equal(t, expectedCID, resultCID)
}

func TestResolveInput_InvalidInput(t *testing.T) {
	ctx := context.Background()
	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			return namesys.Result{}, errors.New("invalid path")
		},
	}

	input := "not-a-valid-cid-or-name"
	_, mutableRes, err := resolveInput(ctx, mockNS, input)
	require.Error(t, err)

	// Should attempt resolution and fail
	if mutableRes != nil {
		require.NotEmpty(t, mutableRes.Error)
	}
}

func TestResolveMutablePath_Success(t *testing.T) {
	ctx := context.Background()

	input := "/ipns/example.com"
	resolvedCID := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			resolvedPath, err := path.NewPath("/ipfs/" + resolvedCID)
			require.NoError(t, err)
			return namesys.Result{Path: resolvedPath}, nil
		},
	}

	resultCID, mutableRes, err := resolveMutablePath(ctx, mockNS, input, true)
	require.NoError(t, err)
	require.NotNil(t, mutableRes)
	require.True(t, mutableRes.IsMutableInput)
	require.Equal(t, input, mutableRes.InputPath)
	require.Equal(t, "/ipfs/"+resolvedCID, mutableRes.ResolvedPath)
	require.NotEmpty(t, mutableRes.DiagnosticURL)

	expectedCID, err := cid.Decode(resolvedCID)
	require.NoError(t, err)
	require.Equal(t, expectedCID, resultCID)
}

func TestResolveMutablePath_Failed(t *testing.T) {
	ctx := context.Background()

	input := "/ipns/nonexistent.example.com"
	expectedError := errors.New("no DNSLink record found")

	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			return namesys.Result{}, expectedError
		},
	}

	_, mutableRes, err := resolveMutablePath(ctx, mockNS, input, true)
	require.Error(t, err)
	require.NotNil(t, mutableRes)
	require.Contains(t, mutableRes.Error, expectedError.Error())
	require.NotEmpty(t, mutableRes.DiagnosticURL)
}

func TestResolveMutablePath_DirectIPFSPath(t *testing.T) {
	ctx := context.Background()

	// Direct /ipfs/ path should extract CID without calling namesys
	resolvedCID := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	input := "/ipfs/" + resolvedCID

	mockNS := &mockNameSystem{
		resolveFunc: func(ctx context.Context, p path.Path) (namesys.Result, error) {
			t.Fatal("Should not call Resolve for /ipfs/ path")
			return namesys.Result{}, nil
		},
	}

	resultCID, mutableRes, err := resolveMutablePath(ctx, mockNS, input, false)
	require.NoError(t, err)
	require.Nil(t, mutableRes)

	expectedCID, err := cid.Decode(resolvedCID)
	require.NoError(t, err)
	require.Equal(t, expectedCID, resultCID)
}
