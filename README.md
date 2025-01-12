# Golang Large File Transfer Client-Server

This repository explores and documents best practices for building a Golang client-server system for transferring large files over HTTP.

## Project Goals

* **Efficient File Transfer:** Minimize transfer time and network overhead.
* **Robustness:** Handle large file sizes gracefully utilizing minimum memory via streaming.
* **Scalability:** Design a system that can handle increasing file sizes and transfer rates.

## Client-Server Architecture

* **Server:**
    * Listens for incoming file transfer requests.
    * Handles file uploads and downloads.
    * Implements appropriate error handling and logging.

* **Client:**
    * Initiates file transfer requests to the server.
    * Handles file uploads and downloads.
    * Implements progress reporting and error handling.

## Key Considerations

* **HTTP Method:**
    * **POST** for uploads: Suitable for large files due to its flexibility and ability to handle large payloads.
    * **GET** for downloads: Generally preferred for downloads, but may require workarounds for very large files.
* **Chunking:**
    * Utilize HTTP chunked transfer encoding to stream large files efficiently.
    * Avoid loading the entire file into memory before sending.
* **Connection Management:**
    * Maintain persistent connections to reduce overhead.
    * Implement keep-alive mechanisms.
* **Error Handling:**
    * Handle network errors, timeouts, and unexpected server responses gracefully.
    * Implement retry mechanisms with exponential backoff.
* **Progress Tracking:**
    * Provide progress updates to both client and server.
    * Consider using techniques like progress bars and percentage completion.

## Implementation Steps

1. **Server Implementation:**
    * Create a HTTP server using the `net/http` package.
    * Define API endpoints for file upload and download.
    * Implement file handling logic (e.g., saving to disk, reading from disk).
    * Implement stream handling
    * Implement error handling and logging.

2. **Client Implementation:**
    * Create a HTTP client using the `net/http` package.
    * Implement file upload and download logic.
    * Handle via streaming.
    * Implement progress tracking and reporting.
    * Implement error handling and retries.

3. **Testing and Benchmarking:**
    * Understand the impact of various timeouts on uploads and non-upload endpoints.

## Server details and parameters

* Server is designed to accept API calls (to / endpoint as both GET and POST) and File Uploads (to /upload/ endpoint)
* POST to / will simply write the full contents to a file, but the main point is the / endpoint is subject to a timeout
* The /upload/ endpoint on the other hand isn't

* -debug (enables debug mode)
* -header-timeout (timeout for headers)
* -api-timeout (timeout for API)
* -upload-delay (delay for upload and / endpoint)

## How to test

### Using curl (happy path)

* Run the server (default timeouts and no delays)

```bash
./server -debug
```

```bash
curl -i -X POST -H 'TENANT: HPIEMEA' -H 'ENTITY: DSMPOSOBJECT' -H "Content-Type: multipart/form-data" -F "object=@__filename__" -F "primaryKey=123456789" -F "version=1.0" -F "sensitivity=high" -v --output '/tmp/x.txt'  http://localhost:8980/upload/
```

* -H indicates headers
* -F indicates form elements
* `@filename` indicates read and send data from file

### Using telnet (header timeout and API timeout)

* While curl is great to test the happy path, I do not know a way to just send the headers and test the timeout
* With telnet, the entire protocol can be simulated by hand

* Testing basic GET - test the happy path
* Then, try doing telnet without sending anything more (the header timeout should kick in)

```bash
./server -debug (default timeout of 30s)
./server -debug -header-timeout 1s
./server -debug -header-timeout 0s (no timeout)
```

```bash
telnet localhost 8980

GET / HTTP/1.1
Host: localhost:8980
```

* Use POST to test the Read timeout - test the happy path
* Once headers are sent through the read timeout implemented at the handler level should kick in (hold back the content)

```bash
./server -debug (default timeout of 30s)
./server -debug -api-timeout 1s
./server -debug -api-timeout 0s (no timeout)
```

```bash
POST / HTTP/1.1
Host: localhost:8980
Content-Type: application/json
Content-Length: 45

{
  "name": "test.txt",
  "size": 1000
}
```

## Using client and curl (API timeouts)

* Add artificial delay on the server side (upload-delay to simulate slow API, api-timeout to impact API endpoint but not upload endpoint)

```bash
./server -debug -upload-delay 5ms -api-timeout 5s
```

* Testing API timeouts (this should timeout)

```bash
./client -debug -filename __filename__ -endpoint=http://localhost:8980/

curl -i -X POST -H 'TENANT: HPIEMEA' -H 'ENTITY: DSMPOSOBJECT' -H "Content-Type: multipart/form-data" -F "object=@__filename__" -F "primaryKey=123456789" -F "version=1.0" -F "sensitivity=high" -v --output '/tmp/x.txt'  http://localhost:8980/
```

* Testing Uploads (this should NOT timeout)

```bash
./client -debug -filename __filename__ -endpoint=http://localhost:8980/upload/

curl -i -X POST -H 'TENANT: HPIEMEA' -H 'ENTITY: DSMPOSOBJECT' -H "Content-Type: multipart/form-data" -F "object=@__filename__" -F "primaryKey=123456789" -F "version=1.0" -F "sensitivity=high" -v --output '/tmp/x.txt'  http://localhost:8980/upload/
```
