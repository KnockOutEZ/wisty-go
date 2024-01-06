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
	"strings"

	"github.com/cheggaaa/pb/v3"
	"github.com/urfave/cli"
)

func downloadVideo(link, filename string) error {
	fmt.Printf("%s\n", filename)
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

	// Wrap the response.Body with a proxy reader to update the progress bar
	reader := bar.NewProxyReader(response.Body)

	_, err = io.Copy(output, reader)
	if err != nil {
		return err
	}

	fmt.Println("\nDownload complete!")
	return nil
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
	}

	selectedResolution := resolutionMapping[resolution]
	var videoURL string

	for _, v := range res {
		if int(v["height"].(float64)) == selectedResolution {
			videoURL = v["url"].(string)
			break
		}
	}

	return downloadVideo(videoURL, filename+".mp4")
}

func fetchResolutions(id, resolution, filename string) error {
	fmt.Println("Connecting...")
	fmt.Println("id: " + id)
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

func main() {
	app := cli.NewApp()
	app.Name = "wisty"
	app.Usage = "Wistia video downloader command line tool"
	app.Version = "1.0.0"

	var resolution, name string
	var id cli.StringSlice

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
	}

	app.Action = func(c *cli.Context) error {
		if len(id) == 0 {
			return cli.NewExitError("Missing required argument 'id'. Run 'wisty-go --help' for help.", 1)
		}

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

		return nil
	}

	err := app.Run(os.Args)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
