package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/robertkrimen/otto"
	"github.com/tidwall/gjson"
)

type Expectation struct {
	Type      string   `yaml:"type"`
	Arguments []string `yaml:"arguments"`
}

type Action struct {
	Type      string   `yaml:"type"`
	Arguments []string `yaml:"arguments"`
}

type Request struct {
	Url        string                 `yaml:"url"`
	Headers    map[string]string      `yaml:"headers"`
	Method     string                 `yaml:"method"`
	URLPattern string                 `yaml:"url_pattern"`
	Json       map[string]interface{} `yaml:"json"`
	Data       map[string]string      `yaml:"data"`
}

type Response struct {
	StatusCode   int                          `yaml:"status_code"`
	RespValues   map[string]map[string]string `yaml:"resp_values"`
	Expectations []Expectation                `yaml:"expectations"`
	Actions      []Action                     `yaml:"actions"`
}

type Stage struct {
	Import   string   `yaml:"import"`
	Before   string   `yaml:"before"`
	Name     string   `yaml:"name"`
	Request  Request  `yaml:"request"`
	Response Response `yaml:"response"`
	After    string   `yaml:"after"`
}

type ImportVars struct {
	Vars map[string]string `yaml:"vars"`
}

type Test struct {
	Name        string            `yaml:"test_name"`
	Description string            `yaml:"description"`
	Variables   map[string]string `yaml:"vars"`
	Stages      []Stage           `yaml:"stages"`
	ImportSteps map[string]string `yaml:"imports"`
}

var version string
var interpreter = otto.New()

func main() {
	boolPtr := flag.Bool("v", false, "version")
	flag.Parse()

	if *boolPtr {
		fmt.Printf("version %s\n", version)
		os.Exit(0)
	}
	if len(os.Args) < 2 {
		fmt.Println("No script file given or unexpected arguments supplied  --  imperius [dir_or_filename(s)]...")
		os.Exit(0)
	}

	for x := 1; x < len(os.Args); x++ {

		testFilename := os.Args[x]
		fi, err := os.Stat(testFilename)
		if err != nil {
			fmt.Printf("Error determining file type (Dir or Regular file) - %s", testFilename)
			continue
		}
		switch mode := fi.Mode(); {
		case mode.IsDir():
			root := testFilename
			files, err := ioutil.ReadDir(root)
			if err != nil {
				fmt.Printf("filepath.Walk error - %s", err.Error())
				continue
			}
			for _, file := range files {
				if !file.IsDir() && strings.HasSuffix(file.Name(), ".yaml") {
					runTestFile(fmt.Sprintf("%s/%s", root, file.Name()))
				}
			}
			continue
		case mode.IsRegular():
			runTestFile(testFilename)
		}
	}
}

func runTestFile(testFilename string) {
	testFile, err := ioutil.ReadFile(testFilename)
	if err != nil {
		fmt.Printf("ioutil.Readfile err   #%v ", err)
		os.Exit(0)
	}

	var test Test
	err = yaml.Unmarshal(testFile, &test)
	if err != nil {
		fmt.Printf("Unmarshal: %v", err)
		os.Exit(0)
	}

	// Resolve vars imports
	for key, filename := range test.Variables {
		if key == "Import" {
			file, err := ioutil.ReadFile(filename)
			if err != nil {
				fmt.Printf("Vars Import file - ioutil.Readfile err   #%v ", err)
				os.Exit(0)
			}
			var importVars ImportVars
			err = yaml.Unmarshal(file, &importVars)
			if err != nil {
				fmt.Printf("Unmarshal: %v", err)
				os.Exit(0)
			}
			for key, val := range importVars.Vars {
				if _, ok := test.Variables[key]; !ok {
					test.Variables[key] = val
				}
			}
		}
	}
	// Resolve stage imports
	for index, step := range test.Stages {
		if step.Import != "" {
			partial, err := ioutil.ReadFile(test.ImportSteps[step.Import])
			if err != nil {
				fmt.Printf("ioutil.Readfile err   #%v ", err)
				os.Exit(0)
			}
			var line Stage
			err = yaml.Unmarshal(partial, &line)
			if err != nil {
				fmt.Printf("Unmarshal: %v", err)
				os.Exit(0)
			}
			test.Stages[index] = line
		}
	}

	allErrors := make([]error, 0)
	fmt.Printf("Running Test: %s\n", test.Name)
	for index, step := range test.Stages {
		fmt.Printf("\n-----\nStep %d -- %s\n", index, step.Name)
		errs := performStep(step, test.Variables)
		if len(errs) > 0 {
			allErrors = append(allErrors, errs...)
		}
	}

	fmt.Printf("\n-----\n")
}

func getRequestBody(Json map[string]interface{}, Data map[string]string) string {
	if len(Json) > 0 {
		body, err := json.Marshal(Json)
		if err != nil {
			return ""
		}
		bodyStr := string(body)
		return bodyStr
	} else if len(Data) > 0 {
		mapData := Data
		form := url.Values{}
		for k, v := range mapData {
			form.Add(k, v)
		}
		return form.Encode()
	}
	return ""
}

func performStep(currentStep Stage, testVariables map[string]string) []error {
	runScript(currentStep.Before, currentStep, testVariables)
	hydratedStep := applyTemplate(&currentStep, testVariables)
	urlPattern := hydratedStep.Request.URLPattern
	body := getRequestBody(hydratedStep.Request.Json, hydratedStep.Request.Data)
	statusCode, respBody, err := makeHttpRequest(hydratedStep.Request.Method,
		fmt.Sprintf("%s/%s", hydratedStep.Request.Url, urlPattern), hydratedStep.Request.Headers, body)

	errs := make([]error, 0)
	if err != nil {
		errs = append(errs, err)
	}
	if statusCode > 299 {
		errs = append(errs, errors.New(fmt.Sprintf("http error: %d", statusCode)))
	}

	if values, ok := hydratedStep.Response.RespValues["json"]; ok {
		for name, jsonPosition := range values {
			testVariables[name] = gjson.Get(respBody, jsonPosition).String()
		}
	}

	err = checkResponse(hydratedStep.Response, testVariables, statusCode)
	if err == nil {
		fmt.Printf("PASS - Expectations all within normal parameters.\n")
	} else {
		errs = append(errs, err)
	}

	performActions(hydratedStep.Response.Actions, testVariables, respBody)
	runScript(currentStep.After, currentStep, testVariables)
	return errs
}

func runScript(script string, step Stage, testVariables map[string]string) {
	if script != "" {
		interpreter.Set("script", step)
		interpreter.Set("environment", testVariables)
		_, err := interpreter.Run(script)
		if err != nil {
			panic(err)
		}
	}
}

func applyTemplate(lineInfo *Stage, data map[string]string) *Stage {
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

	var output Stage
	err = json.Unmarshal(tpl.Bytes(), &output)
	if err != nil {
		panic(err)
	}

	return &output
}

func replaceVars(initial string, replacements map[string]string) string {
	for {
		start := strings.Index(initial, "{")
		end := strings.Index(initial, "}")

		if start == -1 || end == -1 || start > end {
			break
		}
		key := initial[start+1 : end]
		if strings.Contains(key, ":") {
			endKey := strings.Index(key, ":")
			key = key[:endKey]
		}
		replaceable := initial[start : end+1]
		initial = strings.Replace(initial, replaceable, replacements[key], -1)
	}
	return initial
}

func makeHttpRequest(method, serviceUrl string, headers map[string]string, reqBody string) (int, string, error) {
	var req *http.Request
	var err error

	var requestBody *strings.Reader = nil
	if reqBody != "" {
		requestBody = strings.NewReader(reqBody)
		req, err = http.NewRequest(method, serviceUrl, requestBody)
	} else {
		req, err = http.NewRequest(method, serviceUrl, nil)
	}

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

func checkResponse(response Response, scriptValues map[string]string, statusCode int) error {
	if statusCode != response.StatusCode {
		errStr := fmt.Sprintf("status code expected %d was not what was returned %d", response.StatusCode, statusCode)
		fmt.Printf("FATAL: %s\n-----\n", errStr)
		os.Exit(0)
	}

	expectations := response.Expectations
	for _, expectation := range expectations {
		switch expectation.Type {
		case "string_equals":
			expected := expectation.Arguments[0]
			actual := scriptValues[expectation.Arguments[1]]
			if expected != actual {
				errStr := fmt.Sprintf("Expected %s to be equal to %s but it's clearly not!!!", expected, actual)
				fmt.Printf("FATAL: %s\n-----\n", errStr)
				os.Exit(0)
			}
		case "string_contains":
			expected := expectation.Arguments[0]
			searchableString := scriptValues[expectation.Arguments[1]]
			if !strings.Contains(searchableString, expected) {
				errStr := fmt.Sprintf("Expected - %s\n to contain '%s' but it clearly does not!!!", searchableString, expected)
				fmt.Printf("FATAL: %s\n-----\n", errStr)
				os.Exit(0)
			}

		}
	}
	return nil
}

func performActions(actions []Action, scriptValues map[string]string, response string) {
	for _, action := range actions {
		switch action.Type {
		case "print":
			for _, argument := range action.Arguments {
				fmt.Printf("%s = %s\n", argument, scriptValues[argument])
			}
		case "print_response":
			fmt.Println("Response:")
			fmt.Println(response)
		}
	}
}
