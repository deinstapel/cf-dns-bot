# cf-dns-bot - Automate DNS records from your kubernetes cluster using cloudflare

This tool allows you to manage DNS records when using cloudflare for example to build 
availability zones dynamically.

## Deployment

- Create a k8s service account with the permissions `get`, `list`, `watch` on `nodes`
- Spawn the container as deployment (only one replica supported at the moment)
- Set CF_API_KEY and CF_API_EMAIL as env variables inside the container

## Usage

To indicate cf-dns-bot that it should manage dns records for a node, add a label:
```
domainmanager.deinstapel.de: "yes"
```

After that, you can add annotations for the node which get reflected into the cloudflare DNS records automatically:
By default, domainmanager will fetch IP addresses for the node by using a DNS lookup on the nodes hostname extracted
from the `kubernetes.io/hostname` label.

### Adding records:

```
$domain/domainmanager: "true"
```

### Removing Records:

```
$domain/domainmanager: "false"
```
