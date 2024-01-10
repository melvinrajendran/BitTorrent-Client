# BitTorrent Client

<img width="1440" alt="Screenshot 2024-01-10 at 2 47 31 PM" src="https://github.com/melvinrajendran/BitTorrent-Client/assets/44681827/b810bed3-8907-48ec-9eda-6f2a9236e108"><br/>

This repository contains the source code for a full-featured BitTorrent v1.0 client, built using Go. This client takes full advantage of Go's concurrency features, which include goroutines and channels, to download and upload files.

## Implemented Features

## Protocol Specification

BitTorrent is a protocol designed to facilitate file transfers among multiple peers across unreliable networks. The core idea is that by distributing the upload load among a set of peers, a client can better utilize its download capacity. This results in quicker downloads compared to a traditional client-server network. To learn more about the protocol, read the [official specification]("https://wiki.theory.org/BitTorrentSpecification").

As far as this project, I adhered to the details contained in the official specification.

## Running the Client

First, clone the repository.

```
git clone ...
```

Next, run the client with the `-compact` flag.

```
go run . -compact
```

This will download the Debian ISO using the `debian-12.4.0-amd64-netinst.iso.torrent` torrent file. This served as my test torrent file throughout this project, as the output file it is sufficiently large but not too large, at 658.5 MB.

The 

## Design Considerations

## Limitations
