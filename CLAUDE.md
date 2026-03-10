This repo serves as a sandbox for vibe coded apps. Each directory is a self-contained application written entirely by an AI coding agent.

You should create a new directory when working on a new app, and work entirely within the subdirectory. All projects _must_ include the following:
- README.md

Choose an appropriate deployment method for the application:
1. For a static web app (where the deployed project consists entirely of HTML, JS, CSS, etc. files) use staticer. This can include where a running an app locally produces a set of static files that is intended to be the final product. staticer can be used in the following manner:
a) Temporary deployment
`staticer deploy`
b) Permanent deployment
`staticer deploy --domain {DOMAIN} --expires never` where domain is a subdomain on one of the following domains: https://baileys.app, https://baileys.dev, https://baileys.page, https://lab.baileys.dev
eg: `staticer deploy --domain broadcast.baileys.dev --expires never`

Use `staticer help` to understand how to use the CLI.

1. For a dynamic web app (typically one that contains some form of a backend) you must:
  a) Generate a Dockerfile for the project
  b) Generate a docker-compose.yaml for the service, include the following labels for the main service:
    - `traefik.enable=true`
    - `traefik.http.routers.{service}.rule=Host(`{domain}`)`
    - `traefik.http.services.{service}.loadbalancer.server.port={port}`
  c) Build the image, targeting `linux/amd64` platform
  d) Push the image to `registry.baileys.dev`

Upon completing the task (this must be confirmed with the user), raise a draft PR against the repo using the `gh` cli tool. The PR must be formatted as following:
Title: Create {service} app
Description:
```md
## Description
{Describe service; what}

## Access
URL: {public URL}

## Deployment
{Steps to deploy service; either docker-compose or staticer}

## Prompt
> {User prompt}
```

