package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v2"
)

type TestRun struct {
	Name string `yaml:"name"`
	Desc string `yaml:"desc"`
}

type Expectation struct {
	Type      string   `yaml:"type"`
	Arguments []string `yaml:"arguments"`
	Fatal     bool     `yaml:"fatal"`
}

type Action struct {
	Type      string   `yaml:"type"`
	Arguments []string `yaml:"arguments"`
}

type ScriptLine struct {
	Name         string                       `yaml:"name"`
	Url          string                       `yaml:"url"`
	Headers      map[string]string            `yaml:"headers"`
	Method       string                       `yaml:"method"`
	URLPattern   string                       `yaml:"url_pattern"`
	Body         string                       `yaml:"body"`
	RespValues   map[string]map[string]string `yaml:"resp_values"`
	Expectations []Expectation                `yaml:"expectations"`
	Actions      []Action                     `yaml:"actions"`
}

type Script struct {
	ScriptValues map[string]string `yaml:"vars"`
	ScriptLines  []ScriptLine      `yaml:"actions"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("No script file given or unexpected arguments supplied  --  imperius [script_filename]")
		os.Exit(0)
	}
	scriptFilename := os.Args[1]

	scriptFile, err := ioutil.ReadFile(scriptFilename)
	if err != nil {
		fmt.Printf("yamlFile.Get err   #%v ", err)
		os.Exit(0)
	}

	var script Script
	err = yaml.Unmarshal(scriptFile, &script)
	if err != nil {
		fmt.Printf("Unmarshal: %v", err)
		os.Exit(0)
	}

	for _, line := range script.ScriptLines {
		fmt.Println("\n-----\nRunning Action:", line.Name)
		vals, errs := runLine(line, script.ScriptValues)
		if len(errs) == 0 {
			for k, v := range vals {
				script.ScriptValues[k] = v
			}
		} else {
			for _, err := range errs {
				fmt.Println(err)
			}
		}
	}
	fmt.Printf("-----\n")
}

func runLine(inLine ScriptLine, values map[string]string) (map[string]string, []error) {
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

	err = checkExpectations(line.Expectations, statusCode)
	if err == nil {
		fmt.Printf("PASS - Expectations all within normal parameters.\n")
	} else {
		errs = append(errs, err)
	}

	performActions(line.Actions, scriptValues)
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

func checkExpectations(expectations []Expectation, statusCode int) error {
	for _, expectation := range expectations {
		switch expectation.Type {
		case "status":
			expected, err := strconv.Atoi(expectation.Arguments[0])
			if err != nil {
				return err
			}
			if expected != statusCode {
				errStr := fmt.Sprintf("status code expected %d was not what was returned %d", expected, statusCode)
				if expectation.Fatal {
					fmt.Printf("FATAL: %s\n-----\n", errStr)
					os.Exit(0)
				}
				return errors.New(errStr)
			}
		}
	}
	return nil
}

func performActions(actions []Action, scriptValues map[string]string) {
	for _, action := range actions {
		switch action.Type {
		case "print":
			for _, argument := range action.Arguments {
				fmt.Printf("%s = %s\n", argument, scriptValues[argument])
			}
		}
	}
}
