package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	Port           = ":8080"
	LogFile        = "logs.log"
	EndpointStream = "/stream_logs"
)

// go run main.go
func main() {
	// go run main.go c
	if len(os.Args) > 1 {
		client()
		os.Exit(0)
	}

	// every 1 second, write the current time to the log file as example of data coming in
	var f *os.File
	var err error
	go func() {
		for {
			time.Sleep(1 * time.Second)
			f, err = os.OpenFile(LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Fatal(err)
			}

			_, err = f.WriteString(time.Now().String() + "\n")
			if err != nil {
				log.Fatal(err)
			}
		}
	}()
	if f != nil {
		defer f.Close()
	}

	http.HandleFunc(EndpointStream, StreamLogs)

	fmt.Println("Listening on " + Port)
	log.Fatal(http.ListenAndServe(Port, nil))
}

func client() {
	fmt.Println("Client")

	e := "http://" + Port + EndpointStream
	fmt.Println("Connecting to:", e)

	resp, err := http.Get(e)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	// print the last tail of logs to add context (useful on a new connection instance)

	logs := TailFile(10)
	for _, log := range logs {
		fmt.Println(log)
	}

	// read the logs
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		if err != nil {
			log.Fatal(err)
		}
		text := string(buf[:n])

		// remove the \n from the end of the line
		text = strings.Replace(text, "\n", "", -1)
		fmt.Println(text)
		// never stops until ctrl+v
	}
}

func TailFile(lines uint64) []string {
	// read the last n lines of a file
	file, err := os.Open(LogFile)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	// get the file size
	stat, err := file.Stat()
	if err != nil {
		log.Fatal(err)
	}
	fileSize := stat.Size()
	fmt.Println("File size:", fileSize)

	totalLines, err := lineCounter(file)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Total lines:", totalLines)

	if lines > uint64(totalLines) {
		lines = uint64(totalLines)
	}
	fmt.Println("Lines to read:", lines)

	file.Seek(0, io.SeekStart)
	reader := bufio.NewReader(file)

	var logs []string
	for i := 0; i < int(totalLines)-int(lines); i++ {
		_, _, err := reader.ReadLine()
		if err != nil {
			log.Fatal(err)
		}
	}

	for {
		line, _, err := reader.ReadLine()
		if err == io.EOF {
			break
		}
		logs = append(logs, string(line))
	}

	return logs

}

func lineCounter(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := r.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case err == io.EOF:
			return count, nil

		case err != nil:
			return count, err
		}
	}
}

// StreamLogs continuously reads logs from a file and sends them to connected clients.
func StreamLogs(w http.ResponseWriter, r *http.Request) {
	// Set headers to keep the connection open for SSE (Server-Sent Events)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Flush ensures data is sent to the client immediately
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Open the log file
	file, err := os.Open("logs.log")
	if err != nil {
		http.Error(w, "Unable to open log file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Seek to the end of the file to read only new log entries
	file.Seek(0, io.SeekEnd)

	// Read new lines from the log file
	reader := bufio.NewReader(file)

	for {
		select {
		// In case client closes the connection, break out of loop
		case <-r.Context().Done():
			return
		default:
			// Try to read a line
			line, err := reader.ReadString('\n')
			if err == nil {
				// Send the log line to the client
				fmt.Fprintf(w, "%s\n", line)
				flusher.Flush() // Send to client immediately
			} else {
				// If no new log is available, wait for a short period before retrying
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}
