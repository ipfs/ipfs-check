# Changelog

All notable changes to this project will be documented in this file.

Note:
* The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
* This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Legend
The following emojis are used to highlight certain changes:
* 🛠 - BREAKING CHANGE.  Action is required if you use this functionality.
* ✨ - Noteworthy change to be aware of.

## [Unreleased]

### Added

### Changed

### Removed

### Fixed

### Security

## [v0.10.0] - 2026-05-14

### Added

- ✨ When every provider record arrives without an address, the UI no longer reports "0 working providers". It flags the records as likely stale and suggests trying a different routing endpoint in Backend Config. The JSON wire format is unchanged.

### Changed

- ✨ CID-only checks keep looking for usable providers past the first 10 records. The backend drains DHT and IPNI until it has 20 providers with at least one multiaddr, capped at 40 attempted records overall. Address-less records no longer fill a result slot, and `FindPeer` enrichment runs in parallel for the full request timeout. The target of 20 matches what Kubo's bitswap collects over a real retrieval.
- ✨ Bitswap dials now reuse addresses the daemon's main host already learned for the target peer, alongside record-supplied addresses and the `FindPeer` fallback. The dial timeout grows from 15s to 30s so NAT hole punches and relay setup can finish. Providers previously rejected with "failed to dial: no addresses" can now succeed.

## [v0.9.2] - 2026-04-27

### Changed

- [boxo v0.39.0](https://github.com/ipfs/boxo/releases/tag/v0.39.0) (from v0.37.0) ([#138](https://github.com/ipfs/ipfs-check/pull/138))
- [go-libp2p v0.48.0](https://github.com/libp2p/go-libp2p/releases/tag/v0.48.0) (from v0.47.0) ([#136](https://github.com/ipfs/ipfs-check/pull/136))
- [go-libp2p-kad-dht v0.39.1](https://github.com/libp2p/go-libp2p-kad-dht/releases/tag/v0.39.1) (from v0.38.0) ([#136](https://github.com/ipfs/ipfs-check/pull/136))
- bumped GitHub Actions to latest majors: `actions/checkout@v6`, `actions/upload-artifact@v7`, `actions/download-artifact@v8`, `actions/upload-pages-artifact@v5`, `actions/deploy-pages@v5`, `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`, `docker/login-action@v4`, `docker/build-push-action@v7` ([#136](https://github.com/ipfs/ipfs-check/pull/136))
- README rewritten for clarity: active voice, scannable headings, and simpler phrasing ([#136](https://github.com/ipfs/ipfs-check/pull/136))

## [v0.9.1] - 2026-02-18

### Changed

- Update `go-libp2p-kad-dht` to v0.36.0 ([#119](https://github.com/ipfs/ipfs-check/pull/119))
- Update dependencies ([#127](https://github.com/ipfs/ipfs-check/pull/127))
- Modernize code ([#128](https://github.com/ipfs/ipfs-check/pull/128))
- Dockerfile uses Go 1.26 ([#129](https://github.com/ipfs/ipfs-check/pull/129))

## [v0.9.0] - 2025-11-19

### Added

- IPNS and DNSLink resolution support ([#114](https://github.com/ipfs/ipfs-check/pull/114))

### Changed

- Update `go-libp2p` v0.45.0, `boxo` v0.35.2, `vole`, `go-log` v2.9.0 ([#115](https://github.com/ipfs/ipfs-check/pull/115))

## [v0.8.0] - 2025-10-30

### Changed

- Update `vole` and `boxo` ([#111](https://github.com/ipfs/ipfs-check/pull/111))

### Fixed

- Inline IPFS logo SVG ([#109](https://github.com/ipfs/ipfs-check/pull/109))

## [v0.7.0] - 2025-09-15

### Added

- `ipfs-check` can be embedded as an iframe ([#102](https://github.com/ipfs/ipfs-check/pull/102))
- Display peer agent version in diagnostics ([#105](https://github.com/ipfs/ipfs-check/pull/105))
- Non-blocking accelerated DHT initialization ([#103](https://github.com/ipfs/ipfs-check/pull/103))

### Changed

- Update dependencies ([#104](https://github.com/ipfs/ipfs-check/pull/104))
- `ci: uci/update-go` ([#101](https://github.com/ipfs/ipfs-check/pull/101))

### Fixed

- Small race fixes ([#106](https://github.com/ipfs/ipfs-check/pull/106))

## [v0.6.0] - 2025-08-04

### Added

- Plausible analytics ([#91](https://github.com/ipfs/ipfs-check/pull/91))
- Event for checks ([#93](https://github.com/ipfs/ipfs-check/pull/93))

### Changed

- Update dependencies ([#94](https://github.com/ipfs/ipfs-check/pull/94))

### Fixed

- Plausible metrics ([#92](https://github.com/ipfs/ipfs-check/pull/92))
- Fail gracefully when Plausible is blocked ([#97](https://github.com/ipfs/ipfs-check/pull/97))
- Refine errors for private multiaddr dial failures ([#98](https://github.com/ipfs/ipfs-check/pull/98))

## [v0.5.0] - 2025-06-03

### Added

- HTTP retrieval check ([#87](https://github.com/ipfs/ipfs-check/pull/87))
- Overhauled IPFS Check frontend with card-based results and enhanced visual feedback ([#89](https://github.com/ipfs/ipfs-check/pull/89))

### Changed

- Update dependencies ([#86](https://github.com/ipfs/ipfs-check/pull/86))
- `ci: uci/update-go` ([#82](https://github.com/ipfs/ipfs-check/pull/82))
- `ci: uci/copy-templates` ([#83](https://github.com/ipfs/ipfs-check/pull/83))

### Fixed

- Docker image ([#88](https://github.com/ipfs/ipfs-check/pull/88))

## [v0.4.0] - 2024-10-07

### Added

- Filter Bitswap providers in IPNI client using [IPIP-484](https://github.com/ipfs/specs/pull/484) ([#72](https://github.com/ipfs/ipfs-check/pull/72))

### Fixed

- Limit providers from each source to half of max so results from both DHT and IPNI are returned ([#71](https://github.com/ipfs/ipfs-check/pull/71))

## [v0.3.0] - 2024-09-21

### Added

- IPNI checks ([#66](https://github.com/ipfs/ipfs-check/pull/66))
- Accept a CID or a multihash to lookup ([#68](https://github.com/ipfs/ipfs-check/pull/68))

## [v0.2.0] - 2024-09-05

### Added

- CID-only check (in addition to the existing peer-scoped check) ([#47](https://github.com/ipfs/ipfs-check/pull/47))
- Request timeout slider ([#57](https://github.com/ipfs/ipfs-check/pull/57))
- Docker image workflow ([#58](https://github.com/ipfs/ipfs-check/pull/58))
- Find peer when no multiaddrs are supplied ([#62](https://github.com/ipfs/ipfs-check/pull/62))

### Changed

- Rename `CIDinDHT` to `ProviderRecordFromPeerInDHT` ([#52](https://github.com/ipfs/ipfs-check/pull/52))
- Improve stdout UX and run test frontend at `/web` ([#55](https://github.com/ipfs/ipfs-check/pull/55))
- Document `go install` and Docker usage in README ([#61](https://github.com/ipfs/ipfs-check/pull/61))
- `ci: uci/update-go` ([#51](https://github.com/ipfs/ipfs-check/pull/51))

## [v0.1.0] - 2024-08-22

First release of IPFS Check, a debugging tool for checking the retrievability of a CID from an IPFS peer.

### Added

- Timing information for the Bitswap check ([#1](https://github.com/ipfs/ipfs-check/pull/1))
- User-friendly website ([#2](https://github.com/ipfs/ipfs-check/pull/2))
- Spruced website ([#3](https://github.com/ipfs/ipfs-check/pull/3))
- Form validation and mobile-friendly scaling ([#4](https://github.com/ipfs/ipfs-check/pull/4))
- Pointer towards the Accelerated DHT client ([#7](https://github.com/ipfs/ipfs-check/pull/7))
- Documentation on how to find a peer's multiaddr ([#11](https://github.com/ipfs/ipfs-check/pull/11))
- Encode form data in URL query for shareable links ([#21](https://github.com/ipfs/ipfs-check/pull/21))
- Dockerfile ([#20](https://github.com/ipfs/ipfs-check/pull/20))
- Telemetry ([#30](https://github.com/ipfs/ipfs-check/pull/30))
- Hole punching for the test host ([#39](https://github.com/ipfs/ipfs-check/pull/39))
- Connection multiaddr field showing which address was used for the connection ([#38](https://github.com/ipfs/ipfs-check/pull/38))

### Changed

- Loop through found addrs manually and insert newlines ([#10](https://github.com/ipfs/ipfs-check/pull/10))
- Extract daemon and update to Go 1.19 ([#33](https://github.com/ipfs/ipfs-check/pull/33))
- Update dependencies and Go version ([#12](https://github.com/ipfs/ipfs-check/pull/12), [#35](https://github.com/ipfs/ipfs-check/pull/35), [#41](https://github.com/ipfs/ipfs-check/pull/41))
- Query by PeerID rather than full multiaddr ([#15](https://github.com/ipfs/ipfs-check/pull/15))
- `ci: uci/copy-templates` ([#36](https://github.com/ipfs/ipfs-check/pull/36))

### Fixed

- libp2p initialization errors ([#14](https://github.com/ipfs/ipfs-check/pull/14))
- Context timeout ([#37](https://github.com/ipfs/ipfs-check/pull/37))
- Typos ([#5](https://github.com/ipfs/ipfs-check/pull/5), [#32](https://github.com/ipfs/ipfs-check/pull/32))
- Return all check connection multiaddrs ([#45](https://github.com/ipfs/ipfs-check/pull/45))
- Tests ([#46](https://github.com/ipfs/ipfs-check/pull/46))
- Mobile view: overflown output is now scrollable ([#49](https://github.com/ipfs/ipfs-check/pull/49))
