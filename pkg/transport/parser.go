// pkg/transport/parser.go
package transport

import (
	"bytes"
	"fmt"
	"strconv"
)

// MCPSplitFunc implements bufio.SplitFunc for the Model Context Protocol.
// It safely segments the stdio stream using Content-Length headers.
func MCPSplitFunc(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}

	// 1. Locate the header/body boundary (\r\n\r\n)
	separator := []byte("\r\n\r\n")
	sepIdx := bytes.Index(data, separator)
	if sepIdx == -1 {
		// Header not fully read yet. Request more bytes.
		if atEOF {
			return 0, nil, fmt.Errorf("incomplete MCP header at EOF")
		}
		return 0, nil, nil
	}

	// 2. Extract and parse the Content-Length
	headerBlock := data[:sepIdx]
	contentLengthPrefix := []byte("Content-Length: ")
	
	// Ensure we find the Content-Length prefix
	prefixIdx := bytes.Index(headerBlock, contentLengthPrefix)
	if prefixIdx == -1 {
		return 0, nil, fmt.Errorf("invalid MCP header format: missing Content-Length prefix")
	}

	lengthStr := string(bytes.TrimSpace(headerBlock[prefixIdx+len(contentLengthPrefix):]))
	contentLength, err := strconv.Atoi(lengthStr)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid Content-Length value '%s': %v", lengthStr, err)
	}

	// 3. Verify the buffer contains the complete JSON payload
	totalMessageLength := sepIdx + len(separator) + contentLength
	if len(data) < totalMessageLength {
		// Incomplete payload. The JSON is cut off. Wait for next TCP/stdio buffer flush.
		if atEOF {
			return 0, nil, fmt.Errorf("incomplete JSON payload at EOF")
		}
		return 0, nil, nil
	}

	// 4. Extract the pure JSON payload
	jsonPayload := make([]byte, contentLength)
	copy(jsonPayload, data[sepIdx+len(separator):totalMessageLength])

	// Advance the scanner past the header and the payload, return the isolated JSON
	return totalMessageLength, jsonPayload, nil
}