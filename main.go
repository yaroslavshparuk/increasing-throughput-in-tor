package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/xuri/excelize/v2"
)

type Config struct {
	TorProxy  string `json:"torProxy"`
	TargetURL string `json:"targetURL"`
	Threads   int    `json:"threads"`
	FileSize  int    `json:"fileSize"`
}

func main() {
	config, err := readConfig("configs/config.json")
	if err != nil {
		fmt.Println("Error reading configuration file:", err)
		return
	}
	fileName := "Statistics.xlsx"
	f, err := excelize.OpenFile(fileName)
	if err != nil {
		fmt.Println(err)
		f = excelize.NewFile()
	}
	repeats := 22
	sheetName := "Filesize " + fmt.Sprint(config.FileSize) + "Mb, " + fmt.Sprint(config.Threads) + " threads"
	for i := 2; i < repeats; i++ {
		timeElapsedInSeconds := downloadFileFromTorNetwork(config)
		fmt.Println("Elapsed " + fmt.Sprint(timeElapsedInSeconds) + " seconds for attempt " + fmt.Sprint(i-1))
		index, err := f.NewSheet(sheetName)
		if err != nil {
			fmt.Println(err)
			return
		}
		f.SetCellValue(sheetName, "A1", "File size in Mb")
		f.SetCellValue(sheetName, "B1", "Threads")
		f.SetCellValue(sheetName, "C1", "Time in sec")

		f.SetCellValue(sheetName, "A"+fmt.Sprint(i), fmt.Sprint(config.FileSize))
		f.SetCellValue(sheetName, "B"+fmt.Sprint(i), fmt.Sprint(config.Threads))
		f.SetCellValue(sheetName, "C"+fmt.Sprint(i), fmt.Sprintf("%.2f", timeElapsedInSeconds))

		f.SetActiveSheet(index)
		if err := f.SaveAs(fileName); err != nil {
			fmt.Println(err)
		}
	}
}

func downloadFileFromTorNetwork(config Config) float64 {
	startTime := time.Now()
	fmt.Println("Start Time:", startTime)
	config.TargetURL = config.TargetURL + fmt.Sprint(config.FileSize) + "Mb.txt"

	contentLength, err := getFileSize(config)
	if err != nil {
		fmt.Println("Error getting file size:", err)
		return -1
	}

	var wg sync.WaitGroup
	wg.Add(config.Threads)
	partSize := contentLength / int64(config.Threads)
	partsDone := make(chan struct{})
	var partErrs []error

	for i := 0; i < config.Threads; i++ {
		start := int64(i) * partSize
		end := start + partSize - 1
		if i == config.Threads-1 {
			end = contentLength - 1
		}
		go func(partNum int) {
			defer wg.Done()
			filename := fmt.Sprintf("part%d.txt", partNum+1)
			err := downloadPart(config, start, end, filename)
			if err != nil {
				partErrs = append(partErrs, fmt.Errorf("Error downloading part%d: %v", partNum+1, err))
			}
			partsDone <- struct{}{}
		}(i)
	}

	go func() {
		wg.Wait()
		close(partsDone)
	}()

	for range partsDone {
	}

	for _, err := range partErrs {
		if err != nil {
			fmt.Println(err)
			return -1
		}
	}

	err = combineFiles(config.Threads, "full.txt")
	if err != nil {
		fmt.Println("Error combining files:", err)
		return -1
	}

	for i := 1; i <= config.Threads; i++ {
		filename := fmt.Sprintf("part%d.txt", i)
		err = os.Remove(filename)
		if err != nil {
			fmt.Printf("Error removing %s: %v\n", filename, err)
		}
	}

	endTime := time.Now()
	fmt.Println("End Time:", endTime)
	return endTime.Sub(startTime).Seconds()
}

func readConfig(configFile string) (Config, error) {
	file, err := os.Open(configFile)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	var config Config
	err = json.NewDecoder(file).Decode(&config)
	if err != nil {
		return Config{}, err
	}

	return config, nil
}

func getFileSize(config Config) (int64, error) {
	proxyURL, err := url.Parse(config.TorProxy)
	if err != nil {
		return 0, err
	}

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	resp, err := client.Head(config.TargetURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	contentLength := resp.ContentLength
	return contentLength, nil
}

func downloadPart(config Config, start, end int64, filename string) error {
	proxyURL, err := url.Parse(config.TorProxy)
	if err != nil {
		return err
	}

	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	req, err := http.NewRequest("GET", config.TargetURL, nil)
	if err != nil {
		return err
	}

	rangeHeader := fmt.Sprintf("bytes=%d-%d", start, end)
	req.Header.Set("Range", rangeHeader)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	return err
}

func combineFiles(numParts int, outFile string) error {
	out, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer out.Close()

	for i := 1; i <= numParts; i++ {
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
