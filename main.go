// Copyright 2015 bat authors
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

// Bat is a Go implemented CLI cURL-like tool for humans
// bat [flags] [METHOD] URL [ITEM [ITEM]]
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/shoenig/ignore"
)

var version = "v0"

const (
	printReqHeader uint8 = 1 << (iota)
	printReqBody
	printRespHeader
	printRespBody
)

var (
	ver              bool
	form             bool
	pretty           bool
	download         bool
	insecureSSL      bool
	auth             string
	proxy            string
	printV           string
	printOption      uint8
	body             string
	bench            bool
	benchN           int
	benchC           int
	isjson           = flag.Bool("json", true, "Send the data as a JSON object")
	method           = flag.String("method", "GET", "HTTP method")
	URL              = flag.String("url", "", "HTTP request URL")
	jsonmap          map[string]interface{}
	contentJsonRegex = `application/(.*)json`
)

func init() {
	flag.BoolVar(&ver, "v", false, "Print Version Number")
	flag.BoolVar(&ver, "version", false, "Print Version Number")
	flag.BoolVar(&pretty, "pretty", true, "Print Json Pretty Format")
	flag.StringVar(&printV, "p", "HBh", "Print request and response")
	flag.BoolVar(&form, "form", false, "Submitting as a form")
	flag.BoolVar(&form, "f", false, "Submitting as a form")
	flag.BoolVar(&download, "download", false, "Download the url content as file")
	flag.BoolVar(&download, "d", false, "Download the url content as file")
	flag.BoolVar(&insecureSSL, "insecure", false, "Allow connections to SSL sites without certs")
	flag.BoolVar(&insecureSSL, "i", false, "Allow connections to SSL sites without certs")
	flag.StringVar(&auth, "auth", "", "HTTP authentication username:password, USER[:PASS]")
	flag.StringVar(&auth, "a", "", "HTTP authentication username:password, USER[:PASS]")
	flag.StringVar(&proxy, "x", "", "Proxy host and port, PROXY_URL")
	flag.BoolVar(&bench, "bench", false, "Sends bench requests to URL")
	flag.BoolVar(&bench, "b", false, "Sends bench requests to URL")
	flag.IntVar(&benchN, "b.N", 1000, "Number of requests to run")
	flag.IntVar(&benchC, "b.C", 100, "Number of requests to run concurrently.")
	flag.StringVar(&body, "body", "", "Raw data send as body")
	jsonmap = make(map[string]interface{})
}

func parsePrintOption(s string) {
	if strings.ContainsRune(s, 'A') {
		printOption = printReqHeader | printReqBody | printRespHeader | printRespBody
		return
	}
	if strings.ContainsRune(s, 'H') {
		printOption |= printReqHeader
	}
	if strings.ContainsRune(s, 'B') {
		printOption |= printReqBody
	}
	if strings.ContainsRune(s, 'h') {
		printOption |= printRespHeader
	}
	if strings.ContainsRune(s, 'b') {
		printOption |= printRespBody
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) > 0 {
		args = filter(args)
	}
	if ver {
		fmt.Println("Version:", version)
		os.Exit(2)
	}
	parsePrintOption(printV)
	if printOption&printReqBody != printReqBody {
		defaultSetting.DumpBody = false
	}
	var stdin []byte
	if runtime.GOOS != "windows" {
		fi, err := os.Stdin.Stat()
		if err != nil {
			panic(err)
		}
		if fi.Size() != 0 {
			stdin, err = io.ReadAll(os.Stdin)
			if err != nil {
				log.Fatal("Read from Stdin", err)
			}
		}
	}

	if *URL == "" {
		usage()
	}
	if strings.HasPrefix(*URL, ":") {
		urlB := []byte(*URL)
		if *URL == ":" {
			*URL = "http://localhost/"
		} else if len(*URL) > 1 && urlB[1] != '/' {
			*URL = "http://localhost" + *URL
		} else {
			*URL = "http://localhost" + string(urlB[1:])
		}
	}
	if !strings.HasPrefix(*URL, "http://") && !strings.HasPrefix(*URL, "https://") {
		*URL = "http://" + *URL
	}
	u, err := url.Parse(*URL)
	if err != nil {
		log.Fatal(err)
	}
	if auth != "" {
		userpass := strings.Split(auth, ":")
		if len(userpass) == 2 {
			u.User = url.UserPassword(userpass[0], userpass[1])
		} else {
			u.User = url.User(auth)
		}
	}
	*URL = u.String()
	httpreq := getHTTP(*method, *URL, args)
	if u.User != nil {
		password, _ := u.User.Password()
		httpreq.GetRequest().SetBasicAuth(u.User.Username(), password)
	}
	// Insecure SSL Support
	if insecureSSL {
		httpreq.SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true})
	}
	// Proxy Support
	if proxy != "" {
		if !strings.Contains(proxy, "://") && proxy != "" {
			proxy = "http://" + proxy
		}
		purl, err := url.Parse(proxy)
		if err != nil {
			log.Fatal("Proxy Url parse err", err)
		}
		httpreq.SetProxy(purl)
	} else {
		envProxy := getEnvAny("HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy")
		if !strings.Contains(envProxy, "://") && envProxy != "" {
			envProxy = "http://" + envProxy
		}
		envURL, err := url.Parse(envProxy)
		if err != nil {
			log.Fatal("Environment Proxy Url parse err", err)
		}
		httpreq.SetProxy(envURL)
	}
	if body != "" {
		httpreq.Body(body)
	}
	if len(stdin) > 0 {
		var j interface{}
		d := json.NewDecoder(bytes.NewReader(stdin))
		d.UseNumber()
		err = d.Decode(&j)
		if err != nil {
			httpreq.Body(stdin)
		} else {
			_, _ = httpreq.JsonBody(j)
		}
	}

	// AB bench
	if bench {
		httpreq.Debug(false)
		runBench(httpreq)
		return
	}
	res, err := httpreq.Response()
	if err != nil {
		log.Fatalln("can't get the url", err)
	}
	defer func() {
		_ = res.Body.Close()
	}()

	// download file
	if download {
		var fl string
		if disposition := res.Header.Get("Content-Disposition"); disposition != "" {
			fls := strings.Split(disposition, ";")
			for _, f := range fls {
				f = strings.TrimSpace(f)
				if strings.HasPrefix(f, "filename=") {
					// Remove 'filename='
					f = strings.TrimPrefix(f, "filename=")

					// Remove quotes and spaces from either end
					f = strings.TrimLeft(f, "\"' ")
					fl = strings.TrimRight(f, "\"' ")
				}
			}
		}
		if fl == "" {
			_, fl = filepath.Split(u.Path)
		}
		fd, err := os.OpenFile(fl, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			log.Fatal("can't create file", err)
		}
		if runtime.GOOS != "windows" {
			fmt.Println(Color(res.Proto, Magenta), Color(res.Status, Green))
			for k, v := range res.Header {
				fmt.Println(Color(k, Cyan), ":", Color(strings.Join(v, " "), White))
			}
		} else {
			fmt.Println(res.Proto, res.Status)
			for k, v := range res.Header {
				fmt.Println(k, ":", strings.Join(v, " "))
			}
		}
		fmt.Println("")
		contentLength := res.Header.Get("Content-Length")
		var total int64
		if contentLength != "" {
			total, _ = strconv.ParseInt(contentLength, 10, 64)
		}
		fmt.Printf("Downloading to \"%s\"\n", fl)
		pb := NewProgressBar(total)
		pb.Start()
		multiWriter := io.MultiWriter(fd, pb)
		_, err = io.Copy(multiWriter, res.Body)
		if err != nil {
			log.Fatal("Can't Write the body into file", err)
		}
		pb.Finish()
		defer ignore.Close(fd)
		defer ignore.Close(res.Body)
		return
	}

	if runtime.GOOS != "windows" {
		fi, err := os.Stdout.Stat()
		if err != nil {
			panic(err)
		}
		if fi.Mode()&os.ModeDevice == os.ModeDevice {
			var dumpHeader, dumpBody []byte
			dump := httpreq.DumpRequest()
			dps := strings.Split(string(dump), "\n")
			for i, line := range dps {
				if len(strings.Trim(line, "\r\n ")) == 0 {
					dumpHeader = []byte(strings.Join(dps[:i], "\n"))
					dumpBody = []byte(strings.Join(dps[i:], "\n"))
					break
				}
			}
			if printOption&printReqHeader == printReqHeader {
				fmt.Println(ColorfulRequest(string(dumpHeader)))
				fmt.Println("")
			}
			if printOption&printReqBody == printReqBody {
				if string(dumpBody) != "\r\n" {
					fmt.Println(string(dumpBody))
					fmt.Println("")
				}
			}
			if printOption&printRespHeader == printRespHeader {
				fmt.Println(Color(res.Proto, Magenta), Color(res.Status, Green))
				for k, v := range res.Header {
					fmt.Printf("%s: %s\n", Color(k, Cyan), Color(strings.Join(v, " "), White))
				}
				fmt.Println("")
			}
			if printOption&printRespBody == printRespBody {
				body := formatResponseBody(res, httpreq, pretty)
				fmt.Println(ColorfulResponse(body, res.Header.Get("Content-Type")))
			}
		} else {
			body := formatResponseBody(res, httpreq, pretty)
			_, err = os.Stdout.WriteString(body)
			if err != nil {
				log.Fatal(err)
			}
		}
	} else {
		var dumpHeader, dumpBody []byte
		dump := httpreq.DumpRequest()
		dps := strings.Split(string(dump), "\n")
		for i, line := range dps {
			if len(strings.Trim(line, "\r\n ")) == 0 {
				dumpHeader = []byte(strings.Join(dps[:i], "\n"))
				dumpBody = []byte(strings.Join(dps[i:], "\n"))
				break
			}
		}
		if printOption&printReqHeader == printReqHeader {
			fmt.Println(string(dumpHeader))
			fmt.Println("")
		}
		if printOption&printReqBody == printReqBody {
			fmt.Println(string(dumpBody))
			fmt.Println("")
		}
		if printOption&printRespHeader == printRespHeader {
			fmt.Println(res.Proto, res.Status)
			for k, v := range res.Header {
				fmt.Println(k, ":", strings.Join(v, " "))
			}
			fmt.Println("")
		}
		if printOption&printRespBody == printRespBody {
			body := formatResponseBody(res, httpreq, pretty)
			fmt.Println(body)
		}
	}
}

var usageInfo string = `gurl is a Go implemented CLI cURL-like tool for humans.

Usage:

	gurl [flags] [METHOD] URL [ITEM [ITEM]]

flags:
  -a, -auth=USER[:PASS]       Pass a username:password pair as the argument
  -b, -bench=false            Sends bench requests to URL
  -b.N=1000                   Number of requests to run
  -b.C=100                    Number of requests to run concurrently
  -body=""                    Send RAW data as body
  -f, -form=false             Submitting the data as a form
  -j, -json=true              Send the data in a JSON object
  -pretty=true                Print Json Pretty Format
  -i, -insecure=false         Allow connections to SSL sites without certs
  -x=PROXY_URL                Proxy with host and port
  -p="HBh"                    String specifying what the output should contain
         "A" all information
         "H" request headers
         "B" request body
         "h" response headers
         "b" response body
  -v, -version=true           Show Version Number

METHOD:
  gurl defaults to either GET (if there is no request data) or POST (with request data).

URL:
  The only information needed to perform a request is a URL. The default scheme is http://,
  which can be omitted from the argument; example.org works just fine.

ITEM:
  Can be any of:
    Query string   key=value
    Header         key:value
    Post data      key=value
    File upload    key@/path/file

Example:

	gurl example.com
`

func usage() {
	fmt.Print(usageInfo)
	os.Exit(2)
}

func getEnvAny(names ...string) string {
	for _, n := range names {
		if val := os.Getenv(n); val != "" {
			return val
		}
	}
	return ""
}
