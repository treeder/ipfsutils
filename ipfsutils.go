package ipfsutils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"

	ipfsapi "github.com/ipfs/go-ipfs-api"
	"github.com/treeder/gotils/v2"
)

var (
	ipfs   *ipfsapi.Shell
	ipfsUp bool
)

func isUp() bool {
	if ipfs != nil {
		return ipfsUp
	}
	ipfs = ipfsapi.NewShell("localhost:5001")
	ipfsUp = ipfs.IsUp() // could recheck this every few minutes
	if !ipfsUp {
		fmt.Println("ipfs daemon not running, using infura")
	}
	return ipfsUp
}

func UploadFileToIPFS(ctx context.Context, filePath string) (string, error) {
	file, _ := os.Open(filePath)
	defer file.Close()

	body := &bytes.Buffer{}
	// writer := multipart.NewWriter(body)
	// part, _ := writer.CreateFormFile("file", filepath.Base(file.Name()))
	io.Copy(body, file)
	// writer.Close()

	return UploadBytesToIPFS(ctx, body.Bytes())
}

func UploadObjectToIPFS(ctx context.Context, data interface{}) (string, error) {
	jsonValue, err := json.Marshal(data)
	if err != nil {
		return "", gotils.C(ctx).Errorf(": %v", err)
	}

	return UploadBytesToIPFS(ctx, jsonValue)
}

func UploadBytesToIPFS(ctx context.Context, data []byte) (string, error) {
	// try local first
	if isUp() {
		buf := bytes.NewBuffer(data)
		cid, err := ipfs.Add(buf, ipfsapi.CidVersion(1))
		if err != nil {
			return "", err
		}
		// 		fmt.Printf("added %s\n", cid)
		return cid, nil
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", "somefile")
	part.Write(data)
	writer.Close()

	return postToInfura(ctx, writer, body)
}

type InfuraIPFSResponse struct {
	// {
	// 	"Name": "sample-result.json",
	// 	"Hash": "QmSTkR1kkqMuGEeBS49dxVJjgHRMH6cUYa7D3tcHDQ3ea3",
	// 	"Size": "2120"
	// }
	Name string
	Hash string
	Size string
}

func postToInfura(ctx context.Context, writer *multipart.Writer, body io.Reader) (string, error) {
	r, _ := http.NewRequest("POST", "https://ipfs.infura.io:5001/api/v0/add?pin=true", body)
	r.Header.Add("Content-Type", writer.FormDataContentType())
	client := &http.Client{}
	resp, err := client.Do(r)
	var bodyContent []byte
	if err != nil {
		return "", err
	} else {
		// fmt.Println(resp.StatusCode)
		// fmt.Println(resp.Header)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", fmt.Errorf("got status code %v back from IPFS server", resp.StatusCode)
		}
		bodyContent, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", gotils.C(ctx).Errorf("error unmarshaling message from infura: %v", err)
		}
		defer resp.Body.Close()
		// 		fmt.Println(string(bodyContent))
	}
	ipfsResp := &InfuraIPFSResponse{}
	err = json.Unmarshal(bodyContent, ipfsResp)
	if err != nil {
		return "", gotils.C(ctx).Errorf("error unmarshaling message from infura: %v", err)
	}
	cid := ipfsResp.Hash
	// fire off a couple gets to ipfs.io and cloudflare so they cache it quicker
	go gotils.GetString("https://ipfs.io/ipfs/" + cid)
	go gotils.GetString("https://cloudflare-ipfs.com/ipfs/" + cid)
	return cid, nil
}

func GetBytesFromIPFS(ctx context.Context, cid string) ([]byte, error) {
	var stateBytes []byte
	var err error
	if isUp() {
		// todo: need to add a timeout here, this can take a while if file is not local
		// todo: the ipfs lib might need to chanage to support, should bre able to pass a context into all these methods
		reader, err := ipfs.Cat(cid)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		stateBytes, err = ioutil.ReadAll(reader)
		if err != nil {
			return nil, err
		}
	} else {
		// try http gateway
		stateBytes, err = gotils.GetBytes("https://cloudflare-ipfs.com/ipfs/" + cid)
		if err != nil {
			return nil, err
		}
	}
	return stateBytes, nil
}

func GetJSONFromIPFS(ctx context.Context, cid string, t interface{}) error {
	b, err := GetBytesFromIPFS(ctx, cid)
	if err != nil {
		return err
	}
	err = gotils.ParseJSONBytes(b, t)
	if err != nil {
		return err
	}
	return nil
}
