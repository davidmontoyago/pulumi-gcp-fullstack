package gcp

import (
	"fmt"

	compute "github.com/pulumi/pulumi-gcp/sdk/v6/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// creates a best-practice Cloud Armor security policy.
// See:
// https://github.com/GoogleCloudPlatform/terraform-google-cloud-armor/blob/9ea03ee3ff0778a087888582e806da7342635d69/main.tf#L445
func newCloudArmorPolicy(ctx *pulumi.Context, policyName string, args *NetworkArgs, project string) (*compute.SecurityPolicy, error) {
	// Every security policy must have a default rule at priority 2147483647 with match condition *.
	// See:
	// https://cloud.google.com/armor/docs/waf-rules
	defaultRules := newDefaultRule()

	preconfiguredRules := newPreconfiguredRules()

	rules := append(defaultRules, preconfiguredRules...)

	if len(args.ClientIPAllowlist) > 0 {
		// IP allowlist rule to restrict access to a handful of IPs... not for the enterprise
		ipAllowlistRules := newIPAllowlistRules(args.ClientIPAllowlist)
		rules = append(rules, ipAllowlistRules...)
	}

	// TODO allow reCAPTCHA
	// TODO add rate limiting rules
	// TODO add named IP preconfigured rules

	policy, err := compute.NewSecurityPolicy(ctx, fmt.Sprintf("%s-default", policyName), &compute.SecurityPolicyArgs{
		Description: pulumi.String(fmt.Sprintf("Cloud Armor security policy for %s", policyName)),
		Project:     pulumi.String(project),
		Rules:       rules,
		Type:        pulumi.String("CLOUD_ARMOR"),
	})
	return policy, err
}

func newDefaultRule() compute.SecurityPolicyRuleArray {
	var defaultRules compute.SecurityPolicyRuleArray
	defaultRules = append(defaultRules, &compute.SecurityPolicyRuleArgs{
		Action:      pulumi.String("allow"),
		Description: pulumi.String("Default allow rule"),
		Priority:    pulumi.Int(2147483647),
		Match: &compute.SecurityPolicyRuleMatchArgs{
			VersionedExpr: pulumi.String("SRC_IPS_V1"),
			Config: &compute.SecurityPolicyRuleMatchConfigArgs{
				SrcIpRanges: pulumi.StringArray{
					pulumi.String("*"),
				},
			},
		},
	})
	return defaultRules
}

func newIPAllowlistRules(clientIPAllowlist []string) compute.SecurityPolicyRuleArray {
	ipRanges := pulumi.StringArray{}
	for _, ip := range clientIPAllowlist {
		ipRanges = append(ipRanges, pulumi.String(ip))
	}

	var ipAllowlistRules compute.SecurityPolicyRuleArray
	ipAllowlistRules = append(ipAllowlistRules,
		&compute.SecurityPolicyRuleArgs{
			Action:      pulumi.String("allow"),
			Priority:    pulumi.Int(1),
			Description: pulumi.String("IPs allowlist rule"),
			Match: &compute.SecurityPolicyRuleMatchArgs{
				VersionedExpr: pulumi.String("SRC_IPS_V1"),
				Config: &compute.SecurityPolicyRuleMatchConfigArgs{
					SrcIpRanges: ipRanges,
				},
			},
		}, &compute.SecurityPolicyRuleArgs{
			Action:      pulumi.String("deny(403)"),
			Description: pulumi.String("Default IP fallback deny rule"),
			Priority:    pulumi.Int(2),
			Match: &compute.SecurityPolicyRuleMatchArgs{
				VersionedExpr: pulumi.String("SRC_IPS_V1"),
				Config: &compute.SecurityPolicyRuleMatchConfigArgs{
					SrcIpRanges: pulumi.StringArray{
						pulumi.String("*"),
					},
				},
			},
		})
	return ipAllowlistRules
}

// newPreconfiguredRules returns a list of best-practice rules to deny traffic
func newPreconfiguredRules() compute.SecurityPolicyRuleArray {
	var preconfiguredRules compute.SecurityPolicyRuleArray
	for i, rule := range []string{
		"sqli-v33-stable",
		"xss-v33-stable",
		"lfi-v33-stable",
		"rfi-v33-stable",
		"rce-v33-stable",
		"methodenforcement-v33-stable",
		"scannerdetection-v33-stable",
		"protocolattack-v33-stable",
		"sessionfixation-v33-stable",
		"nodejs-v33-stable",
	} {
		preconfiguredWafRule := fmt.Sprintf("evaluatePreconfiguredWaf('%s', {'sensitivity': 1})", rule)
		preconfiguredRules = append(preconfiguredRules, &compute.SecurityPolicyRuleArgs{
			Action:      pulumi.String("deny(502)"),
			Description: pulumi.String(fmt.Sprintf("preconfigured waf rule %s", rule)),
			Priority:    pulumi.Int(20 + i),
			Match: &compute.SecurityPolicyRuleMatchArgs{
				Expr: &compute.SecurityPolicyRuleMatchExprArgs{
					Expression: pulumi.String(preconfiguredWafRule),
				},
			},
		})
	}
	return preconfiguredRules
}
