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
	"os"
	"strconv"
	"strings"

	"github.com/robertkrimen/otto"
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

type Stage struct {
	Import       string                       `yaml:"import"`
	Before       string                       `yaml:"before"`
	Name         string                       `yaml:"name"`
	Url          string                       `yaml:"url"`
	Headers      map[string]string            `yaml:"headers"`
	Method       string                       `yaml:"method"`
	URLPattern   string                       `yaml:"url_pattern"`
	Body         string                       `yaml:"body"`
	RespValues   map[string]map[string]string `yaml:"resp_values"`
	Expectations []Expectation                `yaml:"expectations"`
	Actions      []Action                     `yaml:"actions"`
	After        string                       `yaml:"after"`
}

type Test struct {
	Name        string            `yaml:"test_name"`
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
	if len(os.Args) != 2 {
		fmt.Println("No script file given or unexpected arguments supplied  --  imperius [script_filename]")
		os.Exit(0)
	}

	testFilename := os.Args[1]

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

	//resolve imports
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

	if len(allErrors) != 0 {
		fmt.Printf("-----\n\n")
		for _, err := range allErrors {
			fmt.Println(err.Error())
		}
	} else {
		fmt.Printf("\n-----\n\nNo errors were detected during this test run.\n")
	}
	fmt.Printf("\n-----\n")
}

func performStep(currentStep Stage, testVariables map[string]string) []error {
	runScript(currentStep.Before, currentStep, testVariables)
	hydratedStep := applyTemplate(&currentStep, testVariables)
	urlPattern := hydratedStep.URLPattern
	body := &hydratedStep.Body
	statusCode, respBody, err := makeHttpRequest(hydratedStep.Method, fmt.Sprintf("%s/%s", hydratedStep.Url, urlPattern), hydratedStep.Headers, body)

	errs := make([]error, 0)
	if err != nil {
		errs = append(errs, err)
	}
	if statusCode > 299 {
		errs = append(errs, errors.New(fmt.Sprintf("http error: %d", statusCode)))
	}

	if values, ok := hydratedStep.RespValues["json"]; ok {
		for name, jsonPosition := range values {
			testVariables[name] = gjson.Get(respBody, jsonPosition).String()
		}
	}

	err = checkExpectations(hydratedStep.Expectations, testVariables, statusCode)
	if err == nil {
		fmt.Printf("PASS - Expectations all within normal parameters.\n")
	} else {
		errs = append(errs, err)
	}

	performActions(hydratedStep.Actions, testVariables, respBody)
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

func checkExpectations(expectations []Expectation, scriptValues map[string]string, statusCode int) error {
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
		case "string_equals":
			expected := expectation.Arguments[0]
			actual := scriptValues[expectation.Arguments[1]]
			if expected != actual {
				errStr := fmt.Sprintf("Expected %s to be equalt to %s but it's clearly not!!!", expected, actual)
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
