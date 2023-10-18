# pulumi-gcp-fullstack

Pulumi [Component](https://www.pulumi.com/docs/concepts/resources/components/#component-resources) to easily deploy a serverless fullstack app (frontend and backend) in GCP, and securely publish it to the internet.

Includes:

1. A backend Cloud Run instance.
    - Env config loaded from Secret Manager
1. A frontend Cloud Run instance.
    - Env config loaded from Secret Manager
1. An global HTTPs load balancer ([Classic Application Load Balancer](https://cloud.google.com/load-balancing/docs/https#global-classic-connections)) to control access to the frontend (and the backend).
    - A Google-managed certificate.
    - Optional: default best-practice Cloud Armor policy.
    - Optional: restrict access to an allowlist of IPs.

## Install

```
go get github.com/davidmontoyago/pulumi-gcp-fullstack
```

## Quickstart

```
mystack, err = gcp.NewFullStack(ctx, "my-gcp-stack", &gcp.FullStackArgs{
    Project:       "my-gcp-project",
    Region:        "us-central1",
    BackendImage:  "us-docker.pkg.dev/my-gcp-project/my-app/backend",
    FrontendImage: "us-docker.pkg.dev/my-gcp-project/my-app/frontend",
    Network: &gcp.NetworkArgs{
        DomainURL:        "myapp.example.org",
        EnableCloudArmor: true,
    },
})

```
