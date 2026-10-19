// Package connector deploys tunnel connectors for private Cloud Run services.
//
// A tunnel connector is a long-running proxy process (e.g. cloudflared) that opens outbound-only
// connections to an external network, such as an edge or an overlay, and relays its traffic to
// Cloud Run services with internal-only ingress. It needs no public IP, load balancer or open port.
//
// Connectors run as Cloud Run worker pools with Direct VPC egress (private-ranges-only) on a
// network with Private Google Access and a private run.app DNS zone, so service traffic stays
// internal while edge traffic uses Cloud Run's built-in internet egress.
//
// See: docs/design/tunnel-connector.md
package connector
