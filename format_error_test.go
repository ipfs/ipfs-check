package main

import (
	"errors"
	"testing"

	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestFormatConnectionError(t *testing.T) {
	t.Run("gater disallows connection - all private addresses", func(t *testing.T) {
		err := errors.New("failed to dial: no good addresses\n* [/ip4/10.157.217.171/tcp/4001] gater disallows connection to peer\n* [/ip4/127.0.0.1/tcp/4001] gater disallows connection to peer")

		addrs := []multiaddr.Multiaddr{}
		privAddr1, _ := multiaddr.NewMultiaddr("/ip4/10.157.217.171/tcp/4001")
		privAddr2, _ := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/4001")
		addrs = append(addrs, privAddr1, privAddr2)

		result := formatConnectionError(err, addrs)
		expected := "failed to dial: all addresses (2) are private/local and cannot be reached from the public internet"
		require.Equal(t, expected, result)
	})

	t.Run("gater disallows connection - mixed private and public addresses", func(t *testing.T) {
		err := errors.New("failed to dial: no good addresses\n* [/ip4/10.157.217.171/tcp/4001] gater disallows connection to peer\n* [/ip4/8.8.8.8/tcp/4001] some other error")

		addrs := []multiaddr.Multiaddr{}
		privAddr, _ := multiaddr.NewMultiaddr("/ip4/10.157.217.171/tcp/4001")
		pubAddr, _ := multiaddr.NewMultiaddr("/ip4/8.8.8.8/tcp/4001")
		addrs = append(addrs, privAddr, pubAddr)

		result := formatConnectionError(err, addrs)
		expected := "failed to dial: no good addresses (1 private addresses filtered out)"
		require.Equal(t, expected, result)
	})

	t.Run("gater disallows connection - no private addresses detected", func(t *testing.T) {
		err := errors.New("failed to dial: no good addresses\n* [/ip4/8.8.8.8/tcp/4001] gater disallows connection to peer")

		addrs := []multiaddr.Multiaddr{}
		pubAddr, _ := multiaddr.NewMultiaddr("/ip4/8.8.8.8/tcp/4001")
		addrs = append(addrs, pubAddr)

		result := formatConnectionError(err, addrs)
		// Should return original error since no private addresses were detected
		require.Equal(t, err.Error(), result)
	})

	t.Run("non-gater error", func(t *testing.T) {
		err := errors.New("some other connection error")

		addrs := []multiaddr.Multiaddr{}
		privAddr, _ := multiaddr.NewMultiaddr("/ip4/10.157.217.171/tcp/4001")
		addrs = append(addrs, privAddr)

		result := formatConnectionError(err, addrs)
		// Should return original error for non-gater errors
		require.Equal(t, err.Error(), result)
	})

	t.Run("gater disallows connection - with IPv6 private addresses", func(t *testing.T) {
		err := errors.New("failed to dial: no good addresses\n* [/ip6/::1/tcp/4001] gater disallows connection to peer\n* [/ip6/fc00:bbbb:bbbb:bb01:d:0:1d:d9ab/tcp/4001] gater disallows connection to peer")

		addrs := []multiaddr.Multiaddr{}
		privAddr1, _ := multiaddr.NewMultiaddr("/ip6/::1/tcp/4001")
		privAddr2, _ := multiaddr.NewMultiaddr("/ip6/fc00:bbbb:bbbb:bb01:d:0:1d:d9ab/tcp/4001")
		addrs = append(addrs, privAddr1, privAddr2)

		result := formatConnectionError(err, addrs)
		expected := "failed to dial: all addresses (2) are private/local and cannot be reached from the public internet"
		require.Equal(t, expected, result)
	})
}
