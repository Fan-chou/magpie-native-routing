// Package testutil provides clearly synthetic data for tests only.
package testutil

// SyntheticRouteJSON is a desensitized JSONL record with the shape Magpie
// writes. IDs, names and errors are invented and must never be presented as
// real observations.
const SyntheticRouteJSON = `{"seq":7,"id":7001,"time":"2026-10-18T10:00:00Z","agent":"codex","session":"synthetic-session","model":"synthetic/alpha","provider":"synthetic","group":{"id":"demo","name":"Demo Group","routing":"order","members":["synthetic/alpha","synthetic/beta"]},"order":[{"id":"synthetic","provider":"synthetic","name":"Synthetic Provider","kind":"key","who":"test-key","model":"alpha"},{"id":"backup","provider":"backup","name":"Backup Provider","kind":"key","who":"backup-key","model":"beta"}],"tries":[{"id":"synthetic","model":"alpha","start":"2026-10-18T10:00:00Z","done":true,"status":500,"ms":40,"fail":"synthetic failure"},{"id":"backup","model":"beta","start":"2026-10-18T10:00:00.050Z","done":true,"status":200,"ms":90}],"done":true,"status":200,"ms":150,"ttft":80,"tokens":42,"out":12,"usage":[{"provider":"backup","model":"beta","in":30,"out":12}]}`

// SyntheticSecondRouteJSON is a second, shorter route for merge/tail tests.
const SyntheticSecondRouteJSON = `{"seq":8,"id":7002,"time":"2026-10-18T10:01:00Z","agent":"claude","session":"synthetic-two","model":"synthetic/gamma","order":[],"tries":[],"done":true,"status":200,"ms":10}`
