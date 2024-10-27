# domainforge

  DomainForge is a low-overhead tool that allows you to manage local domains and their associated ports. It streamlines setting up secure local development environments with HTTPS.

  ## Requirements

- [caddy-server](https://caddyserver.com/download): Acts as the local web server with automatic HTTPS.
- [Go](https://golang.org/): Used to install domainforge as it’s a Go-based tool.


## Installation

<!--To install domainforge, you have two options-->

### To install Run:

```
go install github.com/triplemcoder14/domainforge@latest
```

<!---Script installation:-->

<!--```sh
curl -sSL https://raw.githubusercontent.com/triplemcoder14/domainforge/master/install.sh | sudo sh
```-->

## Usage

 Prerequisite
 
 Ensure caddy-server is installed and running.

 Commands
 
- Start the service (foreground mode):

```
domainforge start
```

- Start the service (detached mode):

```
domainforge start -d
```
- Add a new domain (e.g., hello.local on port 3000):

 ```
 domainforge add hello --port 3000
 ```
 Access it at https://hello.local
 
- Remove a domain name

 ```
 domainforge remove hello
 ```
- List all configured domains:

````
domainforge list
````

- Stop the service

````
localbase stop
````



