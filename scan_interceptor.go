/*
 * The Clammit application intercepts HTTP POST requests including content-type
 * "multipart/form-data", forwards any "file" form-data elements to ClamAV
 * and only forwards the request to the application if ClamAV passes all
 * of these elements as virus-free.
 */
package main

import (
	"clammit/scanner"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
)

// The implementation of the Scan interceptor
type ScanInterceptor struct {
	VirusStatusCode int
	Scanner         scanner.Scanner
}

/*
 * Interceptor implementation
 *
 * Runs a multi-part parser across the request body and sends all file contents to Scanner
 *
 * returns True if the body contains a virus
 */
func (c *ScanInterceptor) Handle(w http.ResponseWriter, req *http.Request, body io.Reader) bool {
	//
	// Don't care unless we have some content. When the length is unknown, the length will be -1,
	// but we attempt anyway to read the body.
	//
	if req.ContentLength == 0 {
		if ctx.Config.App.Debug {
			ctx.Logger.Println("Not handling request with zero length")
		}
		return false
	}

	ctx.Logger.Printf("New request %s %s len %d from %s (%s)\n", req.Method, req.URL.Path, req.ContentLength, req.RemoteAddr, req.Header.Get("X-Forwarded-For"))

	//
	// Find any attachments
	//
	contentType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		ctx.Logger.Println("Unable to parse media type:", err)
		return false
	}

	if contentType == "multipart/form-data" {
		boundary := params["boundary"]
		if boundary == "" {
			ctx.Logger.Println("Multipart boundary is not defined")
			return false
		}

		reader := multipart.NewReader(body, boundary)

		//
		// Scan them
		//
		count := 0
		for {
			if part, err := reader.NextPart(); err != nil {
				if err == io.EOF {
					break // all done
				}
				ctx.Logger.Println("Error parsing multipart form:", err)
				http.Error(w, "Bad Request", 400)
				return true
			} else {
				defer part.Close()
				if part.FileName() != "" {
					count++
					if ctx.Config.App.Debug {
						ctx.Logger.Println("Scanning", part.FileName())
					}
					if responded := c.respondOnVirus(w, part.FileName(), part); responded == true {
						return true
					}
				}
			}
		}
		if ctx.Config.App.Debug {
			ctx.Logger.Printf("Processed %d form parts", count)
		}
	} else {
		filename := "untitled"
		_, params, err := mime.ParseMediaType(req.Header.Get("Content-Disposition"))
		if err == nil {
			filename = params["filename"]
		}
		return c.respondOnVirus(w, filename, body)
	}
	return false
}

/*
 * This function performs the virus scan and handles the http response in case of a virus.
 *
 * returns True if a virus has been found and a http error response has been written
 */
func (c *ScanInterceptor) respondOnVirus(w http.ResponseWriter, filename string, reader io.Reader) bool {
	if hasVirus, err := c.Scanner.HasVirus(reader); err != nil {
		ctx.Logger.Printf("Unable to scan file (%s): %v\n", filename, err)
		return false
	} else if hasVirus {
		w.WriteHeader(c.VirusStatusCode)
		w.Write([]byte(fmt.Sprintf("File %s has a virus!", filename)))
		return true
	}
	return false
}
