package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Metadata struct {
	FileName string   `json:"filename"`
	FileSize int64    `json:"filesize"`
	Peers    []string `json:"peers"`
}

var metadataCache []Metadata
var torBridge = "socks5://138.68.150.252:9050"

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "default")
}
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	fileName := r.URL.Query().Get("filename")
	file, err := os.Open(fileName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fileStat, err := file.Stat()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileStat.Size()))

	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
func metadataHandler(w http.ResponseWriter, r *http.Request) {
	queryString := r.URL.Query()
	fileName := queryString.Get("filename")
	if fileName == "" {
		http.Error(w, "filename is required", http.StatusBadRequest)
	}

	fileMetadata, errorFromGet := getOrCreateMetadateFor(fileName)
	if errorFromGet != nil {
		http.Error(w, errorFromGet.Error(), http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "application/json")
	errorFromDecode := json.NewEncoder(w).Encode(fileMetadata)
	if errorFromDecode != nil {
		http.Error(w, errorFromDecode.Error(), http.StatusInternalServerError)
	}
}
func readFileContent(filePath string) (string, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", filePath)
	}

	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("error reading file: %s", err)
	}

	return strings.TrimSuffix(string(content), "\n"), nil
}
func getOrCreateMetadateFor(fileName string) (*Metadata, error) {
	for _, file := range metadataCache {
		if file.FileName == fileName {
			return &file, nil
		}
	}
	fileContent, _ := os.Open(fileName)
	defer fileContent.Close()
	stats, _ := fileContent.Stat()
	onionHostNameFile := "/var/lib/tor/hidden_service/hostname"
	onionHostName, err := readFileContent(onionHostNameFile)
	if err != nil {
		return nil, err
	}
	newMetadata := Metadata{
		FileName: fileName,
		FileSize: stats.Size(),
		Peers:    []string{"http://" + onionHostName},
	}
	metadataCache = append(metadataCache, newMetadata)
	return &newMetadata, nil
}

func downloadFile(metadata *Metadata) {
	torBridgeURL, err := url.Parse(torBridge)
	if err != nil {
		return
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(torBridgeURL)}}
	urlWithParam := fmt.Sprintf("%s?filename=%s", metadata.Peers[0]+"/download", metadata.FileName)
	downloadRequest, err := http.NewRequest(http.MethodGet, urlWithParam, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return
	}

	downloadResponse, err := client.Do(downloadRequest)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return
	}

	defer downloadResponse.Body.Close()
	responseBody, err := ioutil.ReadAll(downloadResponse.Body)

	if err != nil {
		fmt.Println("Error reading response body:", err)
		return
	}
	fmt.Println(string(responseBody))
}
func downloadMetadata(baseUrl string) (*Metadata, error) {
	torBridgeURL, err := url.Parse(torBridge)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(torBridgeURL)}}
	urlWithParam := fmt.Sprintf("%s?filename=%s", baseUrl+"/metadata", "test.txt")
	metadataRequest, err := http.NewRequest(http.MethodGet, urlWithParam, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return nil, err
	}

	metadataResponse, err := client.Do(metadataRequest)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return nil, err
	}

	defer metadataResponse.Body.Close()
	responseBody, err := ioutil.ReadAll(metadataResponse.Body)

	if err != nil {
		fmt.Println("Error reading response body:", err)
		return nil, err
	}
	var metadata Metadata
	err = json.Unmarshal(responseBody, &metadata)
	if err != nil {
		fmt.Println("Error parsing response body:", err)
		return nil, err
	}

	fmt.Printf("Status code: %d\nSize: %d\n", metadataResponse.StatusCode, metadata.FileSize)
	return &metadata, nil
}
func mergeUniqueSlices(slice1, slice2 []string) []string {
	valueSet := make(map[string]struct{})

	for _, value := range slice1 {
		valueSet[value] = struct{}{}
	}
	for _, value := range slice2 {
		valueSet[value] = struct{}{}
	}

	uniqueSlice := make([]string, 0, len(valueSet))
	for key := range valueSet {
		uniqueSlice = append(uniqueSlice, key)
	}
	return uniqueSlice
}
func cacheMetadata(metadata *Metadata) {
	for i, file := range metadataCache {
		if file.FileName == metadata.FileName {
			metadataCache[i].Peers = mergeUniqueSlices(file.Peers, metadata.Peers)
			return
		}
	}

	metadataCache = append(metadataCache, *metadata)
}
func main() {
	port := ":8080"

	http.HandleFunc("/", defaultHandler)
	http.HandleFunc("/download", downloadHandler)
	http.HandleFunc("/metadata", metadataHandler)
	go func() {
		fmt.Printf("Server listening on port %s\n", port)
		fmt.Println("Waiting for requests...")
		http.ListenAndServe(port, nil)
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Println("\nSelect action (download/exit):")
		if !scanner.Scan() {
			break
		}
		action := scanner.Text()

		switch action {
		case "download":
			fmt.Println("Enter peer address:")
			if !scanner.Scan() {
				break
			}

			fileMetadata, err := downloadMetadata(scanner.Text())
			if err != nil {
				fmt.Println("Error downloading file metadata:", err)
			}
			downloadFile(fileMetadata)
			cacheMetadata(fileMetadata)
		case "exit":
			fmt.Println("Exiting...")
			os.Exit(0)
		default:
			fmt.Println("Invalid action. Please choose server, client, or exit.")
		}
	}
}
