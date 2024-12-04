package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/cheggaaa/pb/v3"
	"github.com/urfave/cli"
)

type VideoItem struct {
	Index       int    `json:"index"`
	DynamicPart string `json:"dynamic-part"`
	Downloaded  bool   `json:"downloaded"`
	Name        string `json:"name"`
	Type        string `json:"type"`
}

type CourseData struct {
	Name      string      `json:"name"`
	ItemCount int         `json:"item-count"`
	Items     []VideoItem `json:"items"`
}

func downloadVideo(link, filename string) error {
	fmt.Printf("Downloading: %s\n", filename)
	output, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer output.Close()

	response, err := http.Get(link)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	bar := pb.Full.Start64(response.ContentLength)
	defer bar.Finish()

	reader := bar.NewProxyReader(response.Body)

	_, err = io.Copy(output, reader)
	if err != nil {
		return err
	}

	fmt.Println("\nDownload complete!")
	return nil
}

func downloadFromJSON(jsonFile string) error {
	fileContent, err := ioutil.ReadFile(jsonFile)
	if err != nil {
		return err
	}

	var courseData CourseData
	err = json.Unmarshal(fileContent, &courseData)
	if err != nil {
		return err
	}

	resolutions := []string{"1080p", "720p", "716p", "540p", "480p", "360p", "220p"}
	var failedDownloads []string

	for i := range courseData.Items {
		item := &courseData.Items[i]
		
		if item.Downloaded || item.Type != "video" {
			continue
		}

		coursePath := filepath.Join(".", courseData.Name)
		if err := os.MkdirAll(coursePath, 0755); err != nil {
			fmt.Printf("Error creating directory for %s: %v\nSkipping...\n", item.Name, err)
			failedDownloads = append(failedDownloads, item.Name)
			continue
		}

		cleanName := strings.ReplaceAll(item.Name, "/", "_")
		filename := filepath.Join(coursePath, cleanName + ".mp4")

		success := false
		
		availableResolutions, err := getAvailableResolutions(item.DynamicPart)
		if err != nil {
			fmt.Printf("Warning: Could not get available resolutions for %s: %v\nFalling back to default resolution list\n", item.Name, err)
			availableResolutions = resolutions
		}

		for _, res := range availableResolutions {
			fmt.Printf("Trying resolution %s for %s\n", res, item.Name)
			err = fetchResolutions(item.DynamicPart, res, filename)
			if err == nil {
				success = true
				break
			}
			fmt.Printf("%s not available, trying next resolution\n", res)
		}

		if !success {
			fmt.Printf("Failed to download %s after trying all resolutions\nSkipping to next video...\n\n", item.Name)
			failedDownloads = append(failedDownloads, item.Name)
			continue
		}

		item.Downloaded = true

		updatedContent, err := json.MarshalIndent(courseData, "", "  ")
		if err != nil {
			fmt.Printf("Warning: Could not update progress for %s: %v\n", item.Name, err)
		} else {
			if err := ioutil.WriteFile(jsonFile, updatedContent, 0644); err != nil {
				fmt.Printf("Warning: Could not save progress for %s: %v\n", item.Name, err)
			}
		}
	}

	if len(failedDownloads) > 0 {
		fmt.Printf("\nThe following videos failed to download:\n")
		for _, name := range failedDownloads {
			fmt.Printf("- %s\n", name)
		}
		fmt.Println("\nYou can try downloading these videos again later.")
		return fmt.Errorf("some videos failed to download")
	}

	return nil
}

func getAvailableResolutions(id string) ([]string, error) {
	url := "http://fast.wistia.net/embed/iframe/" + id
	response, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	var content []map[string]interface{}
	re := regexp.MustCompile(`"assets":(\[.*?\])`)
	match := re.FindStringSubmatch(string(body))

	if len(match) > 1 {
		if err := json.Unmarshal([]byte(match[1]), &content); err != nil {
			return nil, err
		}

		resolutions := make([]string, 0)
		for _, asset := range content {
			if height, ok := asset["height"].(float64); ok {
				resolutions = append(resolutions, fmt.Sprintf("%dp", int(height)))
			}
		}

		sort.Slice(resolutions, func(i, j int) bool {
			iRes, _ := strconv.Atoi(strings.TrimSuffix(resolutions[i], "p"))
			jRes, _ := strconv.Atoi(strings.TrimSuffix(resolutions[j], "p"))
			return iRes > jRes
		})

		return resolutions, nil
	}

	return nil, fmt.Errorf("no resolution data found")
}

func parseResolution(metadata, resolution, filename string) error {
	file, err := os.Open(metadata)
	if err != nil {
		return err
	}
	defer file.Close()

	var res []map[string]interface{}
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&res)
	if err != nil {
		return err
	}

	resolutionMapping := map[string]int{
		"1080p": 1080,
		"720p":  720,
		"540p":  540,
		"480p":  480,
		"360p":  360,
		"716p":  716,
		"220p":  220,
	}

	selectedResolution := resolutionMapping[resolution]
	var videoURL string

	for _, v := range res {
		heightVal, ok := v["height"]
		if !ok || heightVal == nil {
			continue
		}

		height, ok := heightVal.(float64)
		if !ok {
			continue
		}

		if int(height) == selectedResolution {
			urlVal, ok := v["url"]
			if !ok || urlVal == nil {
				continue
			}

			videoURL, ok = urlVal.(string)
			if !ok || videoURL == "" {
				continue
			}
			break
		}
	}

	if videoURL == "" {
		return fmt.Errorf("resolution %s not found or invalid video data", resolution)
	}

	return downloadVideo(videoURL, filename)
}

func fetchResolutions(id, resolution, filename string) error {
	fmt.Printf("Fetching video ID: %s\n", id)
	url := "http://fast.wistia.net/embed/iframe/" + id
	response, err := http.Get(url)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}

	var content []map[string]interface{}
	re := regexp.MustCompile(`"assets":(\[.*?\])`)
	match := re.FindStringSubmatch(string(body))

	if len(match) > 1 {
		if err := json.Unmarshal([]byte(match[1]), &content); err != nil {
			return err
		}

		withExtract := "extract.json"
		withExtractPath := filepath.Join(".", withExtract)

		file, err := os.Create(withExtractPath)
		if err != nil {
			return err
		}
		defer file.Close()

		encoder := json.NewEncoder(file)
		if err := encoder.Encode(content); err != nil {
			return err
		}

		if err := parseResolution(withExtract, resolution, filename); err != nil {
			return err
		}

		if err := os.Remove(withExtractPath); err != nil {
			return err
		}
	}

	return nil
}

func verifyDownloads() error {
	jsonFiles, err := filepath.Glob("./jsons/*.json")
	if err != nil {
		return fmt.Errorf("error finding JSON files: %v", err)
	}

	var missingFiles []string
	totalVideos := 0
	downloadedVideos := 0

	for _, jsonFile := range jsonFiles {
		fileContent, err := ioutil.ReadFile(jsonFile)
		if err != nil {
			fmt.Printf("Warning: Could not read %s: %v\n", jsonFile, err)
			continue
		}

		var courseData CourseData
		if err := json.Unmarshal(fileContent, &courseData); err != nil {
			fmt.Printf("Warning: Could not parse %s: %v\n", jsonFile, err)
			continue
		}

		for _, item := range courseData.Items {
			if item.Type != "video" {
				continue
			}

			totalVideos++
			cleanName := strings.ReplaceAll(item.Name, "/", "_")
			expectedPath := filepath.Join(".", courseData.Name, cleanName+".mp4")

			if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
				if item.Downloaded {
					missingFiles = append(missingFiles, fmt.Sprintf("%s (marked as downloaded but file missing)", expectedPath))
				} else {
					missingFiles = append(missingFiles, expectedPath)
				}
			} else {
				downloadedVideos++
			}
		}
	}

	fmt.Printf("\nDownload Status:\n")
	fmt.Printf("Total videos: %d\n", totalVideos)
	fmt.Printf("Downloaded: %d\n", downloadedVideos)
	fmt.Printf("Missing: %d\n", len(missingFiles))

	if len(missingFiles) > 0 {
		fmt.Printf("\nMissing files:\n")
		for _, file := range missingFiles {
			fmt.Printf("- %s\n", file)
		}
		return fmt.Errorf("some files are missing")
	}

	fmt.Printf("\nAll files are downloaded successfully!\n")
	return nil
}

func main() {
	app := cli.NewApp()
	app.Name = "wisty-go"
	app.Usage = "Wistia video downloader command line tool"
	app.Version = "1.0.0"

	var resolution, name string
	var id cli.StringSlice
	var useJSONs, verify bool

	app.Flags = []cli.Flag{
		cli.StringSliceFlag{
			Name:  "id, i",
			Usage: "Wistia video id",
			Value: &id,
		},
		cli.StringFlag{
			Name:        "resolution, r",
			Value:       "1080p",
			Usage:       "Video resolution (e.g., 720p)",
			Destination: &resolution,
		},
		cli.StringFlag{
			Name:        "name, n",
			Value:       "",
			Usage:       "Video name",
			Destination: &name,
		},
		cli.BoolFlag{
			Name:        "jsons",
			Usage:       "Download videos based on JSON files in ./jsons folder",
			Destination: &useJSONs,
		},
		cli.BoolFlag{
			Name:        "verify",
			Usage:       "Verify if all videos from JSON files are downloaded",
			Destination: &verify,
		},
	}

	app.Action = func(c *cli.Context) error {
		if verify {
			return verifyDownloads()
		}

		if useJSONs {
			jsonFiles, err := filepath.Glob("./jsons/*.json")
			if err != nil {
				return err
			}

			var failedFiles []string
			
			for i, jsonFile := range jsonFiles {
				fmt.Printf("\nProcessing file %d/%d: %s\n", i+1, len(jsonFiles), jsonFile)
				err := downloadFromJSON(jsonFile)
				if err != nil {
					fmt.Printf("Error processing %s: %v\nContinuing with next file...\n", jsonFile, err)
					failedFiles = append(failedFiles, jsonFile)
					continue
				}
			}

			if len(failedFiles) > 0 {
				fmt.Printf("\nThe following files had errors:\n")
				for _, file := range failedFiles {
					fmt.Printf("- %s\n", file)
				}
				return fmt.Errorf("some files had errors during processing")
			}
		} else if len(id) > 0 {
			idSlice := strings.Split(id.String(), ",")

			for i, videoID := range idSlice {
				var filename string
				if name != "" {
					filename = fmt.Sprintf("%s/%s%d", ".", name, i+1)
				} else {
					filename = fmt.Sprintf("%d", i+1)
				}

				if err := fetchResolutions(videoID, resolution, filename); err != nil {
					return err
				}
			}
		} else {
			return cli.NewExitError("Either --jsons flag or --id flag is required", 1)
		}

		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}