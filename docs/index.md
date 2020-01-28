![Imperius Curse gif](./imperius.gif)
## Why `imperius`

imperius came about from my usage of tavern testing.  Tavern is an excellent application but it suffers from the need for many dependacies and so it is hard to install and use in a project that is not entirely devoted to python code.  So to test APIs in other languages it was ideal to be able to deploy a single self contained executable that could run tavern tests with only perhaps minimal changes and so `imperius` was created.

## Installation

#### Mac OSX
    brew tap p4tin/tap
    brew install imperius

### Linux
We have provide a `deb` and `rpm` package that can be installed just like any others just download the version you want from the `releases` section on github and run the installer as normal and you are done.

### Windows
I apologize but I have no easy installation for windows users.  Simply download the executable `*.tar.gz` for windows, unpack it and place the imperius.exe in your path somewhere and you are all set.

### Docker

Currently there is no docker support but we plan on creating a docker file that can run all the test files in a specified directory, all includes would have to be placed in a sub-directory.  More to come...  (if someone wanted tackle the Dockerfile and the goreleaser changes needed it would be greatly appreciated.)

## Usage

Once the `imperius` executable is in your path you can simply call it with the command lime command: `imperius` to get the usage.

- imperius -v - with print the current version you are using:
```
>>>imperius -v                      
version v0.3.0
>>>
```

to run a script use the command:
```
imperius <yaml_file_name>
Running Test:  ...

```

## Yaml file structure

There are 4 main sections to the YAML file:

### General
    This section contains only 2 tags:

        - title: printed on the first line of the test run
        - description: for informational purposes only

- Vars  
    In this section you will put variables that will be used in the test stages to replace the variable placeholders.  This allow for repetitive things to be placed at the top of the script (i.e.: URL, Tokens, etc,...  This is simply a key-value list.

    **Future Improvement**  (import a file with pre-defined vars like deployment environment variables (i.e.: dev, staging, prod) That could be switched based on the required environment to be tested at the time.   (kinda like postman environments)

    Example:
    ```
    vars:
        Url: https://httpbin.org
        Tracer: abc1234
    ```
- Imports
    For the moment import file can only contain 1 stage of the entire test.  That stage can be used in the main file, as is, several times.  This was done so that you could re-use the same step several times within a test without having to completely re-define it over and over again.
    Future Enhancement: A way to create a file full of `vars` to be used in the main test file

    Example:
    ```
    imports:
        PostJsonData: test_scripts/post_json_data.yaml
        PostFormData: test_scripts/post_form_data.yaml
    ```
- Stages
    Each stage is a specific call to an API endpoint like `GetAToken`, `AddToBasket`, etc,...  In each stage the yaml has 3 sections: `general`, `request` and `response`

    **General**: This section contains the stage's `name` and the `before` and `after` scripts.  `before` and `after` are written in plain old javascript and they both receive the `environment` which contains all the current variables that exist.

## test-server example w/yaml file
Test server code:

    package main

    import (
        "net/http"
        "time"
    
        "github.com/dgrijalva/jwt-go"
        "github.com/labstack/echo"
        "github.com/labstack/echo/middleware"
    )

    var SECRET = "CGQgaG7GYvTcpaQZqosLy4"
    var db = map[string]int{}
    
    func login(c echo.Context) error {
        username := c.FormValue("username")
        password := c.FormValue("password")
    
        // Throws unauthorized error
        if username != "jon" || password != "shhh!" {
            return echo.ErrUnauthorized
        }
    
        // Create token
        token := jwt.New(jwt.SigningMethodHS256)
    
        // Set claims
        claims := token.Claims.(jwt.MapClaims)
        claims["name"] = "Jon Snow"
        claims["admin"] = true
        claims["exp"] = time.Now().Add(time.Hour * 72).Unix()
    
        // Generate encoded token and send it as response.
        t, err := token.SignedString([]byte(SECRET))
        if err != nil {
            return err
        }
    
        return c.JSON(http.StatusOK, map[string]string{
            "token": t,
        })
    }
    
    func accessible(c echo.Context) error {
        return c.String(http.StatusOK, "Accessible")
    }
    
    type NumbersPayload struct {
        Name   string `json:"name"`
        Number int    `json:"number"`
    }
    
    func post_numbers(c echo.Context) error {
        user := c.Get("user").(*jwt.Token)
        claims := user.Claims.(jwt.MapClaims)
        _ = claims["name"].(string)
    
        payload := new(NumbersPayload)
        if err := c.Bind(payload); err != nil {
            return c.JSON(http.StatusBadRequest, NumbersPayload{})
        }
    
        db[payload.Name] = payload.Number
        return c.JSON(http.StatusCreated, NumbersPayload{
            Name:   payload.Name,
            Number: payload.Number,
        })
    }
    
    func get_numbers(c echo.Context) error {
        user := c.Get("user").(*jwt.Token)
        claims := user.Claims.(jwt.MapClaims)
        _ = claims["name"].(string)
    
        payload := new(NumbersPayload)
        if err := c.Bind(payload); err != nil {
            return c.JSON(http.StatusBadRequest, NumbersPayload{})
        }
    
        if number, ok := db[payload.Name]; !ok {
            return c.JSON(http.StatusNotFound, NumbersPayload{})
        } else {
            return c.JSON(http.StatusOK, NumbersPayload{
                Name:   payload.Name,
                Number: number,
            })
        }
    }
    
    func main() {
        e := echo.New()
    
        // Middleware
        e.Use(middleware.Logger())
        e.Use(middleware.Recover())
    
        // Login route
        e.POST("/login", login)
    
        // Unauthenticated route
        e.GET("/", accessible)
    
        // Restricted group
        r := e.Group("/numbers")
        r.Use(middleware.JWT([]byte(SECRET)))
        r.GET("", get_numbers)
        r.POST("", post_numbers)
    
        e.Logger.Fatal(e.Start(":5000"))
    }

    ```
Yaml file to test this server:

    test_name: "tavern test server tests"
    vars:
      Url: http://localhost:5000
    stages:
      -
        name: public page access
        request:
          method: GET
          url: "{{.Url}}"
          url_pattern: ""
        response:
          expectations:
            - type: status
              arguments:
                - 200
              fatal: true
          actions:
            - type: print_response
      -
        name: Get a number not logged in yet
        request:
          method: GET
          url: "{{.Url}}"
          url_pattern: "numbers"
        headers:
          Content-Type: "application/json"
        response:
          expectations:
            - type: status
              arguments:
                - 400
              fatal: true
          actions:
            - type: print_response
      -
        name: login
        request:
          method: POST
          url: "{{.Url}}"
          url_pattern: "login"
          headers:
            Content-Type: "application/x-www-form-urlencoded"
          data:
            username: "jon"
            password: "shhh!"
        response:
          expectations:
            - type: status
              arguments:
                - 200
              fatal: true
          resp_values:
            json:
              Token: token
          actions:
            - type: print_response
      -
        name: Get a number that is not in the DB
        request:
          method: GET
          url: "{{.Url}}"
          url_pattern: "numbers"
          headers:
            Authorization: Bearer {{.Token}}
            Content-Type: "application/json"
          json:
            name: test1
        response:
          expectations:
            - type: status
              arguments:
                - 404
              fatal: true
          actions:
            - type: print_response
      -
        name: Add a number to the list
        request:
          method: POST
          url: "{{.Url}}"
          url_pattern: "numbers"
          headers:
            Authorization: Bearer {{.Token}}
            Content-Type: "application/json"
          json:
            name: test1
            number: 23
        response:
          expectations:
            - type: status
              arguments:
                - 201
              fatal: true
          actions:
            - type: print_response
      -
        name: Get a number OK
        request:
          method: GET
          url: "{{.Url}}"
          url_pattern: "numbers"
          headers:
            Authorization: Bearer {{.Token}}
            Content-Type: "application/json"
          json:
            name: test1
        response:
          expectations:
            - type: status
              arguments:
                - 200
              fatal: true
          actions:
            - type: print_response
