package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v2"
)

type TestRun struct {
	Name string `yaml:"name"`
	Desc string `yaml:"desc"`
}

type Check struct {
	Type      string   `yaml:"type"`
	Arguments []string `yaml:"arguments"`
}

type ScriptLine struct {
	Url        string                       `yaml:"url"`
	Headers    map[string]string            `yaml:"headers"`
	Method     string                       `yaml:"method"`
	URLPattern string                       `yaml:"url_pattern"`
	Body       string                       `yaml:"body"`
	RespValues map[string]map[string]string `yaml:"resp_values"`
	Checks     []Check                      `yaml:"checks"`
}

type Script struct {
	ScriptValues map[string]string `yaml:"vars"`
	ScriptLines  []ScriptLine      `yaml:"actions"`
}

func main() {
	if len(os.Args) != 2 {
		log.Println("No script file given or unexpected arguments supplied  --  imperius [script_filename]")
		os.Exit(0)
	}
	scriptFilename := os.Args[1]

	scriptFile, err := ioutil.ReadFile(scriptFilename)
	if err != nil {
		log.Printf("yamlFile.Get err   #%v ", err)
	}

	var script Script
	err = yaml.Unmarshal(scriptFile, &script)
	if err != nil {
		log.Fatalf("Unmarshal: %v", err)
	}

	for _, line := range script.ScriptLines {
		vals, errs := runLine(line, script.ScriptValues)
		if len(errs) == 0 {
			for k, v := range vals {
				script.ScriptValues[k] = v
			}
		}
	}

	fmt.Printf("%+v\n", script.ScriptValues)

}

func runLine(inLine ScriptLine, values map[string]string) (map[string]string, []error) {
	fmt.Printf("- %+v\n\t", inLine)
	line := applyTemplate(&inLine, values)
	urlPattern := line.URLPattern
	body := &line.Body
	statusCode, respBoby, err := makeHttpRequest(line.Method, fmt.Sprintf("%s/%s", line.Url, urlPattern), line.Headers, body)

	errs := make([]error, 0)
	if err != nil {
		errs = append(errs, err)
	}
	if statusCode > 299 {
		errs = append(errs, errors.New(fmt.Sprintf("http error: %d", statusCode)))
	}

	scriptValues := make(map[string]string)
	if values, ok := line.RespValues["json"]; ok {
		for name, jsonPosition := range values {
			scriptValues[name] = gjson.Get(respBoby, jsonPosition).String()
		}
	}

	//doChecks(statusCode, respBoby, )

	return scriptValues, errs
}

func applyTemplate(lineInfo *ScriptLine, data map[string]string) *ScriptLine {
	bites, err := json.Marshal(lineInfo)
	if err != nil {
		panic(err)
	}
	tmpl, err := template.New("test").Parse(string(bites))
	if err != nil {
		panic(err)
	}

	var tpl bytes.Buffer
	err = tmpl.Execute(&tpl, data)
	if err != nil {
		panic(err)
	}

	var output ScriptLine
	err = json.Unmarshal(tpl.Bytes(), &output)
	if err != nil {
		panic(err)
	}

	return &output
}

func makeHttpRequest(method, serviceUrl string, headers map[string]string, reqBody *string) (int, string, error) {
	var req *http.Request
	var err error

	var requestBody *strings.Reader = nil
	if reqBody != nil {
		requestBody = strings.NewReader(*reqBody)
	}

	req, err = http.NewRequest(method, serviceUrl, requestBody)
	if err != nil {
		return 500, "", err
	}

	for key, val := range headers {
		req.Header.Set(key, val)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 500, "", err
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 500, "", err
	}
	resp.Body.Close()

	return resp.StatusCode, string(body), nil
}

func doChecks() {

}
