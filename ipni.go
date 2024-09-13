package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/ipni/go-libipni/find/client"
	"github.com/ipni/go-libipni/find/model"
	"github.com/ipni/go-libipni/metadata"
	// "github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
	// "github.com/urfave/cli/v2"
)

func FindIPNIProviders(ctx context.Context, cidArg string, indexer string) error {
	// mhArgs := cctx.StringSlice("mh")
	// cidArgs := cctx.StringSlice("cid")
	// if len(mhArgs) == 0 && len(cidArgs) == 0 {
	// 	return fmt.Errorf("must specify at least one multihash or CID")
	// }

	mhs := make([]multihash.Multihash, 0, 1)
	// for i := range mhArgs {
	// 	m, err := multihash.FromB58String(mhArgs[i])
	// 	if err != nil {
	// 		return err
	// 	}
	// 	mhs = append(mhs, m)
	// }
	c, err := cid.Decode(cidArg)
	if err != nil {
		return err
	}
	mhs = append(mhs, c.Hash())

	// if cctx.Bool("no-priv") {
	// 	return clearFind(cctx, mhs)
	// }
	return dhFind(ctx, mhs, indexer)
}

func dhFind(ctx context.Context, mhs []multihash.Multihash, indexer string) error {
	// cl, err := client.NewDHashClient(
	// 	client.WithProvidersURL(indexers...),
	// 	client.WithDHStoreURL(''),
	// 	client.WithPcacheTTL(0),
	// )

	cl, err := client.New(indexer)
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}

	resp, err := client.FindBatch(ctx, cl, mhs)
	if err != nil {
		return err
	}
	// if resp == nil && cctx.Bool("fallback") {
	// 	return clearFind(cctx, mhs)
	// }
	fmt.Println("🔒 Reader privacy enabled")
	return printResults(ctx, resp)
}

func printResults(ctx context.Context, resp *model.FindResponse) error {
	if resp == nil || len(resp.MultihashResults) == 0 {
		fmt.Println("index not found")
		return nil
	}

	// if cctx.Bool("id-only") {
	// 	seen := make(map[peer.ID]struct{})
	// 	for i := range resp.MultihashResults {
	// 		for _, pr := range resp.MultihashResults[i].ProviderResults {
	// 			if _, ok := seen[pr.Provider.ID]; ok {
	// 				continue
	// 			}
	// 			seen[pr.Provider.ID] = struct{}{}
	// 			fmt.Println(pr.Provider.ID.String())
	// 		}
	// 	}
	// 	return nil
	// }

	for i := range resp.MultihashResults {
		fmt.Println("Multihash:", resp.MultihashResults[i].Multihash.B58String())
		if len(resp.MultihashResults[i].ProviderResults) == 0 {
			fmt.Println("  index not found")
			continue
		}
		// Group results by provider.
		providers := make(map[string][]model.ProviderResult)
		for _, pr := range resp.MultihashResults[i].ProviderResults {
			provStr := pr.Provider.String()
			providers[provStr] = append(providers[provStr], pr)
		}
		for provStr, prs := range providers {
			fmt.Println("  Provider:", provStr)
			for _, pr := range prs {
				fmt.Println("    ContextID:", base64.StdEncoding.EncodeToString(pr.ContextID))
				fmt.Println("      Metadata:", decodeMetadata(pr.Metadata))
			}
		}
	}
	return nil
}

func decodeMetadata(metaBytes []byte) string {
	if len(metaBytes) == 0 {
		return "nil"
	}
	meta := metadata.Default.New()
	err := meta.UnmarshalBinary(metaBytes)
	if err != nil {
		return fmt.Sprint("error: ", err.Error())
	}
	protoStrs := make([]string, meta.Len())
	for i, p := range meta.Protocols() {
		protoStrs[i] = p.String()
	}
	return strings.Join(protoStrs, ", ")
}
