# BitTorrent Client

<img width="1440" alt="Screenshot 2024-01-11 at 5 02 02 PM" src="https://github.com/melvinrajendran/BitTorrent-Client/assets/44681827/4c52957c-3b59-42b3-85de-1189fc9b834c"><br/>

This repository contains the source code for a full-featured BitTorrent v1.0 client, built using Go. This client takes advantage of Go's concurrency features, which include goroutines and channels, to both download and upload files.

## Protocol Specification

BitTorrent is a protocol designed to facilitate file transfers among multiple peers across unreliable networks. The core idea is that by distributing the upload load among a set of peers, a client can better utilize its download capacity. This results in quicker downloads compared to a traditional client-server relationship.

To learn more about the protocol, read the [official specification](https://wiki.theory.org/BitTorrentSpecification).

## Design Considerations

As far as my implementation, I mostly adhered to the official v1.0 specification. The only exception is that my client uses a maximum request queue size of 25, as opposed to the recommended queue size of 10. Based on my testing, this yields significantly faster download and upload speeds without causing the network to become congested.

## Implemented Features

* Can communicate with an HTTP tracker
  * Supports compact tracker responses
  * Supports the tracker "scrape" convention
* Can download files from peers
  * Implements the queueing, random piece selection, and end-game algorithms
  * Speed is comparable to that of the [Transmission BT client](https://transmissionbt.com/)
* Can upload files to peers
  * Implements the choking and optimistic unchoking algorithms

## Limitations

* Only supports `.torrent` files (not magnet links)
* Only supports HTTP trackers (not DHT trackers or peer exchanges)
* Only supports single-file torrent files (not multi-file torrent files)

## Running the Client

First, clone the repository.

```
git clone https://github.com/melvinrajendran/BitTorrent-Client.git
```

Next, run the client with the `-compact` flag.

```
go run . -compact
```

This will download the Debian ISO using the `debian-12.4.0-amd64-netinst.iso.torrent` torrent file. I used this file to test my implementation throughout the project, as the output file is sufficiently large (but not too large) at 658.5 MB.

The client can accept a compact tracker response with the `-compact` flag, the path to another torrent file can be specified with the `-torrent` flag, and the client can optionally print additional logs with the `-verbose` flag.
