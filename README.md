# Go-Kardia

Golang implementation of Kardia chain following the specs in [Technical Paper](http://www.kardiachain.io/paper.pdf)

# Kardia private-net
- Release: [kardia-milestone2-20180904](https://github.com/kardiachain/go-kardia/releases/tag/kardia-milestone2-20180904)
- Build: [Jenkins](http://35.198.226.189:8080/job/go-kardia/)
- UI: [Kardiascan](http://scan.kardiachain.io/)

# Quickstart
### Go environment setup
Install [Go](https://golang.org/doc/install) v1.10 to $HOME directory. Sets environment vars:  
> export GOPATH=$HOME/go  
> export PATH=$PATH:$GOPATH/bin

### Build
Install [dep](https://github.com/golang/dep) v0.5 tool for dependency management.  
Download library dependency and build Kardia binary.
```
cd $GOPATH/src/github.com/kardiachain/go-kardia
dep ensure
go install
```

### Unit tests
```
cd $GOPATH/src/github.com/kardiachain/go-kardia
go test ./...
```
# Run
### Start with docker
See the [docker](https://github.com/kardiachain/go-kardia/tree/master/docker) for more details.

### Monitor with Kardiascan
- Setup [JSON-RPC](https://github.com/kardiachain/go-kardia/tree/master/rpc) request
- Update config to [Kardiascan config ](https://github.com/kardiachain/KardiaScan#update-node-config)
- Launch [Kardiascan](https://github.com/kardiachain/KardiaScan#run-development-mode)

# Key features
### Consensus DPOS-PBFT
Simulate [PBFT](http://pmg.csail.mit.edu/papers/osdi99.pdf) consensus with multiple nodes and different voting strategies.  
See [consensus](https://github.com/kardiachain/go-kardia/tree/master/consensus) for more details.

### Ether-Kardia Dual node
Simulate node participating in both Ether [Rinkeby](https://www.rinkeby.io/#stats) and Kardia network.  
See [dual](https://github.com/kardiachain/go-kardia/tree/master/dual) for more details.

### Kardia Virtual Machine (KVM)
Test Solidity smart contracts via [vm/sample_kvm](https://github.com/kardiachain/go-kardia/tree/master/vm/sample_kvm).

### JSON-RPC API
APIs to communicate with running Kardia node.  
See [rpc](https://github.com/kardiachain/go-kardia/tree/master/rpc) for more details.
