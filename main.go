package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Config        struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"config"`
	Layers []struct {
		MediaType string `json:"mediaType"`
		Size      int64  `json:"size"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:", os.Args[0], "<command> [arguments...]")
		os.Exit(1)
	}
	switch os.Args[1] {
	case "list":
		handleList()
	case "pull":
		handlePull("")
	default:
		fmt.Println("Unknown command:", os.Args[1])
		os.Exit(1)
	}

}

func handleList() {
	if len(os.Args) != 3 {
		fmt.Println("Usage:", os.Args[0], "list", "<repository>")
		os.Exit(1)
	}
	repo := os.Args[2]
	registryHost, imageName, _, err := parseImagePath(repo)
	if err != nil {
		log.Fatalf("Failed to list tags,  %s", err)
	}
	url := "https://" + registryHost + "/v2/" + imageName + "/tags/list"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to list tags,  %s", err)
	}
	client := &http.Client{}
	res, err := client.Do(req)
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		log.Fatalf("Failed to list tags, status code: %d", res.StatusCode)
	}

	var tagsResponse struct {
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(res.Body).Decode(&tagsResponse); err != nil {
		log.Fatalf("Failed to list tags,  %s", err)
	}
	fmt.Println("Tags:")
	for _, digest := range tagsResponse.Tags {
		fmt.Printf("  %s\n", digest)
	}

}

func parseImagePath(imagePath string) (registryHost, imageName, tag string, err error) {

	if !strings.Contains(imagePath, "/") {
		err = fmt.Errorf("invalid image path format: %s", imagePath)
		return
	}
	parts := strings.Split(imagePath, "/")
	registryHost = parts[0]
	imageWithTag := strings.Join(parts[1:], "/")
	if !strings.Contains(imageWithTag, ":") {
		imageName = imageWithTag
		tag = "latest"
	} else {
		imageParts := strings.Split(imageWithTag, ":")
		imageName = imageParts[0]
		tag = imageParts[1]
	}

	return

}

func handlePull(token string) {
	if len(os.Args) != 3 {
		fmt.Println("Usage:", os.Args[0], "pull", "<repository>")
		os.Exit(1)
	}
	repo := os.Args[2]
	registryHost, imageName, tag, err := parseImagePath(repo)
	if err != nil {
		log.Fatalf("Failed to pull,  %s", err)
	}
	url := "https://" + registryHost + "/v2/" + imageName + "/manifests/" + tag
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to get manifests,  %s", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	res, err := client.Do(req)
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusUnauthorized && token == "" {
			token, err := getJWT(res.Header.Get("Www-Authenticate"))
			if err != nil {
				log.Fatalf("Failed to get manifests,Auth Fail: %d", err)
			}
			handlePull(token)
			return
		} else {
			log.Fatalf("Failed to get manifests, status code: %d", res.StatusCode)
		}

	}

	var m Manifest
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		log.Fatalf("Failed to list tags,  %s", err)
	}

	fmt.Println("m:")
	url = fmt.Sprintf("https://%s/v2/%s/blobs/%s", registryHost, imageName, m.Config.Digest)
	name := imageName
	if strings.Contains(imageName, "/") {
		name = imageName[strings.LastIndex(imageName, "/")+1:]
	}
	err = os.MkdirAll("./images/"+registryHost+"/"+name+"/"+tag, 0750)
	if err != nil {
		log.Fatalf("Failed to mkdir,  %s", err)
	}
	err = downloadFileWithProgress(url, "./images/"+registryHost+"/"+name+"/"+tag+"/config.json", m.Config.Size, "config.json", token)
	if err != nil {
		log.Fatalf("Failed to pull layer,  %s", err)
	}
	fmt.Printf("\r%s", strings.Repeat(" ", 35))
	fmt.Printf("\r%s:Download complete!", "config.json")
	fmt.Print("\n")
	for _, v := range m.Layers {
		parts := strings.Split(v.Digest, ":")
		if len(parts) != 2 {
			log.Fatalf("Invalid image:%s", v.Digest)
		}
		url = fmt.Sprintf("https://%s/v2/%s/blobs/%s", registryHost, imageName, v.Digest)
		err = downloadFileWithProgress(url, "./images/"+registryHost+"/"+name+"/"+tag+"/"+parts[1]+".tar", v.Size, parts[1][:5], token)
		if err != nil {
			log.Fatalf("Failed to pull layer,  %s", err)
		}
		fmt.Printf("\r%s", strings.Repeat(" ", 35))
		fmt.Printf("\r%s:Download complete!", parts[1][:5])
		fmt.Print("\n")
	}

}
func downloadFileWithProgress(url string, dest string, Size int64, filename string, token string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to get layers,  %s", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		token, err := getJWT(resp.Header.Get("Www-Authenticate"))
		if err != nil {
			log.Fatalf("Failed to get manifests,Auth Fail: %d", err)
		}
		return downloadFileWithProgress(url, dest, Size, filename, token)
	}
	defer resp.Body.Close()
	s, _ := os.Stat(dest)
	if s != nil {
		if s.Size() == Size {
			return nil
		}
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	counter := &WriteCounter{}
	counter.Size = Size
	counter.filename = filename
	_, err = io.Copy(out, io.TeeReader(resp.Body, counter))
	if err != nil {
		return err
	}

	return nil
}

type WriteCounter struct {
	filename string
	Size     int64
	Total    uint64
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	n := len(p)
	wc.Total += uint64(n)
	wc.PrintProgress()
	return n, nil
}

func (wc WriteCounter) PrintProgress() {
	fmt.Printf("\r%s", strings.Repeat(" ", 40))

	fmt.Printf("\r%s:Downloading %s/%s", wc.filename, humanize(wc.Total), humanize(uint64(wc.Size)))
}

func humanize(bytes uint64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB", "EB"}
	bytes = bytes * uint64(1)
	unitIndex := 0
	for bytes >= 1024 {
		bytes /= 1024
		unitIndex++
	}
	return strconv.FormatInt(int64(bytes), 10) + units[unitIndex]
}
func getJWT(header string) (string, error) {
	header = strings.TrimPrefix(header, "Bearer ")

	headerFields := strings.Split(header, ",")
	realm := ""
	service := ""
	scope := ""
	for _, field := range headerFields {
		if strings.HasPrefix(field, "realm=") {
			realm = strings.Trim(field, "realm=")
			realm = strings.Trim(realm, "\"")
		} else if strings.HasPrefix(field, "service=") {
			service = strings.Trim(field, "service=")
			service = strings.Trim(service, "\"")
		} else if strings.HasPrefix(field, "scope=") {
			scope = strings.Trim(field, "scope=")
			scope = strings.Trim(scope, "\"")
		}
	}
	if realm == "" || scope == "" || service == "" {
		return "", fmt.Errorf("Error parsing WWW-Authenticate header")
	}
	url := realm
	if strings.Contains(url, "?") {
		url = url + "&scope=" + scope + "&service=" + service
	} else {
		url = url + "?cope=" + scope + "&service=" + service
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Failed to get manifests,  %s", err)
	}
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to get manifests,  %s", err)
	}
	defer res.Body.Close()
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return "", err
	}
	return resp.Token, nil
}
