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
	"strconv"
	"strings"
	"sync"
)

type Metadata struct {
	FileName string   `json:"filename"`
	FileSize int64    `json:"filesize"`
	Peers    []string `json:"peers"`
}

var metadataCache []Metadata

var torBridge = "socks5://127.0.0.1:9050"

func defaultHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "default")
}
func downloadHandler(w http.ResponseWriter, r *http.Request) {
	fileName := r.URL.Query().Get("filename")
	start, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	end, err := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	file, err := os.Open(fileName)
	fileStat, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if start < 0 || end > fileStat.Size()-1 || start > end {
		http.Error(w, "Invalid range", http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileStat.Size()))
	sectionReader := io.NewSectionReader(file, start, end-start+1)
	_, err = io.Copy(w, sectionReader)
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
		return
	}

	fileMetadata, errorFromGet := getOrCreateMetadateFor(fileName)
	if errorFromGet != nil {
		http.Error(w, errorFromGet.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	errorFromDecode := json.NewEncoder(w).Encode(fileMetadata)
	if errorFromDecode != nil {
		http.Error(w, errorFromDecode.Error(), http.StatusInternalServerError)
		return
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
	onionHostName, err := getHostOnionName()
	if err != nil {
		fmt.Println(err)
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

func getHostOnionName() (string, error) {
	onionHostNameFile := "/var/lib/tor/hidden_service/hostname"
	onionHostName, err := readFileContent(onionHostNameFile)
	if err != nil || onionHostName == "" {
		return "", err
	}
	return onionHostName, nil
}

func downloadFile(baseUrl string, filename string, start int64, end int64, partNumber int) {
	torBridgeURL, err := url.Parse(torBridge)
	if err != nil {
		return
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(torBridgeURL)}}
	urlWithParam := fmt.Sprintf("%s?filename=%s&start=%d&end=%d", baseUrl+"/download", filename, start, end)
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

	out, err := os.Create(fmt.Sprintf("part%d.txt", partNumber))
	if err != nil {
		fmt.Println("Error creating file:", err)
		return
	}
	defer out.Close()
	_, err = out.Write(responseBody)
	if err != nil {
		fmt.Println("Error writing response:", err)
		return
	}
}
func downloadMetadata(baseUrl string) (*Metadata, error) {
	torBridgeURL, err := url.Parse(torBridge)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(torBridgeURL)}}
	urlWithParam := fmt.Sprintf("%s?filename=%s", baseUrl+"/metadata", "12Mb.txt")
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
	if metadataResponse.StatusCode != http.StatusOK {
		fmt.Println("Error sending request:", metadataResponse.StatusCode)
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

func combineFiles(numParts int, outFile string) error {
	out, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer out.Close()

	for i := 0; i < numParts; i++ {
		filename := fmt.Sprintf("part%d.txt", i)
		f, err := os.Open(filename)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(out, f)
		if err != nil {
			return err
		}
	}

	return nil
}

func main() {
	port := ":80"
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
				break
			}
			var wg sync.WaitGroup = sync.WaitGroup{}
			peersCount := len(fileMetadata.Peers)

			partSize := fileMetadata.FileSize / int64(peersCount)
			for i := 0; i < peersCount; i++ {
				wg.Add(1)
				start := int64(i) * partSize
				end := start + partSize - 1
				go func() {
					defer wg.Done()
					downloadFile(fileMetadata.Peers[0], fileMetadata.FileName, start, end, i)
				}()
			}
			wg.Wait()

			err = combineFiles(peersCount, "full.txt")
			if err != nil {
				fmt.Println("Error combining files:", err)
				break
			}
			for i := 0; i < peersCount; i++ {
				filename := fmt.Sprintf("part%d.txt", i)
				err = os.Remove(filename)
				if err != nil {
					fmt.Printf("Error removing %s: %v\n", filename, err)
				}
			}
			onionHostName, err := getHostOnionName()
			if err != nil {
				fmt.Println("Error in getting onion host name:", err)
			}
			fileMetadata.Peers = append(fileMetadata.Peers, "http://"+onionHostName)
			cacheMetadata(fileMetadata)
		case "exit":
			fmt.Println("Exiting...")
			os.Exit(0)
		default:
			fmt.Println("Invalid action. Please choose server, client, or exit.")
		}
	}
}
