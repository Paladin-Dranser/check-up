package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v2"

	"check-up/bash"
)

// TODO Understand whether it is ok to use package variables for using in internal functions
var verbosity int = 0
var workdir string = ""

//go:embed case.yaml
var yamlConfig []byte

func print(msg string) {
	if os.Getenv("TERM") == "" {
		mod := regexp.MustCompile(`\033[^m]*m`).ReplaceAllString(msg, "")
		mod = regexp.MustCompile(`✓`).ReplaceAllString(mod, "success")
		mod = regexp.MustCompile(`✗`).ReplaceAllString(mod, "FAILURE")
		log.Println(mod)
	} else {
		log.Println(msg)
	}
}

type ScenarioItem struct {
	// YAML-Defined data
	Name         string            `yaml:"name"`
	Case         string            `yaml:"case"`
	GlobalEnv    map[string]string `yaml:"global_env"`
	Env          map[string]string `yaml:"env"`
	Workdir      string            `yaml:"workdir"`
	Description  string            `yaml:"description"`
	Script       string            `yaml:"script"`
	Skip         bool              `yaml:"skip"`
	Output       bool              `yaml:"output"`
	SecretPhrase string            `yaml:"secret_phrase"`
	Weight       int               `yaml:"weight"`
	Log          string            `yaml:"log"`
	Fatal        bool              `yaml:"fatal"`
	Debug        string            `yaml:"debug"`
	Before       []string          `yaml:"before"`
	After        []string          `yaml:"after"`

	// Runtime data
	Status   string
	Result   error
	Stdout   string
	Duration string

	canShow bool
	canRun  bool
}

func (s *ScenarioItem) IsSuccessful() bool {
	return s.Status == "success"
}

func (s *ScenarioItem) IsFailed() bool {
	return s.Status == "failed"
}

func (s *ScenarioItem) CanShow() bool {
	// return s.Case != ""
	return s.canShow
}

func (s *ScenarioItem) RunBash() ([]byte, error) {
	var stdout []byte = []byte("")
	var err error = nil

	if s.Script != "" {
		tmpDir, _ := ioutil.TempDir("/tmp/check-up", "._")
		defer os.RemoveAll(tmpDir)

		tmpFile, _ := ioutil.TempFile(tmpDir, "tmp.*")

		T := struct {
			Script string
		}{
			Script: s.Script,
		}

		tmpl, _ := template.New("bash-script").Parse(string(bash.BashScript))
		tmpl.Execute(tmpFile, T)

		script := exec.Command("/bin/bash", tmpFile.Name())
		script.Dir = workdir
		script.Env = os.Environ()
		for key, value := range s.GlobalEnv {
			script.Env = append(script.Env,
				fmt.Sprintf("%s=%s", key, value),
			)
		}

		stdout, err = script.CombinedOutput()
		s.Stdout = strings.TrimSpace(string(stdout))
		s.Result = err

		if err == nil {
			s.Status = "success"
		} else {
			s.Status = "failed"
		}

		return stdout, err
	}
	return []byte(""), nil
}

type suitConfig struct {
	Name  string         `yaml:"name"`
	Cases []ScenarioItem `yaml:"cases"`

	startTime time.Time
	endTime   time.Time

	all         int
	successfull int
	failed      int
	score       float64
	duration    string
}

func (c *suitConfig) getScenarioIds() []int {
	result := []int{}

	for i := 0; i < len(c.Cases); i++ {
		if c.Cases[i].Skip {
			continue
		}

		if c.Cases[i].canShow || c.Cases[i].canRun {
			result = append(result, i)
		}
	}
	return result
}

func (c *suitConfig) getScenarioCount() int {
	result := 0
	for _, i := range c.getScenarioIds() {
		if c.Cases[i].CanShow() {
			result++
		}
	}
	return result
}

func (c *suitConfig) getIdByName(name string) int {
	for id, item := range c.Cases {
		if item.Name == name {
			return id
		}
	}
	return -1
}

func (c *suitConfig) printHeader() {
	scenariosCount := c.getScenarioCount()

	c.startTime = time.Now()

	if scenariosCount > 1 {
		log.Printf("[ %s ], 1..%d tests\n", c.Name, scenariosCount)
		return
	}

	if scenariosCount == 1 {
		log.Printf("[ %s ], 1 test\n", c.Name)
		return
	}

	if scenariosCount == 0 {
		log.Printf("[ %s ], no tests to run\n", c.Name)
		return
	}
}

func (c *suitConfig) signOff() {
	c.endTime = time.Now()

	sum := 0
	max := 0
	failed := 0
	all := 0

	for _, i := range c.getScenarioIds() {
		item := c.Cases[i]
		if item.CanShow() {
			all++
			max += item.Weight
			if item.IsSuccessful() {
				sum += item.Weight
			} else {
				failed++
			}
		}
	}

	c.successfull = all - failed
	c.failed = failed
	c.all = all
	c.score = 100 * float64(sum) / float64(max)
	c.duration = duration(c.startTime, c.endTime)
}

func (c *suitConfig) printSummary() {
	if c.all > 0 {
		if c.failed > 0 {
			print(fmt.Sprintf("%d (of %d) tests passed, \033[31m%d tests failed,\033[0m rated as %.2f%%, spent %s", c.successfull, c.all, c.failed, c.score, c.duration))
		} else {
			print(fmt.Sprintf("\033[32m%d (of %d) tests passed, %d tests failed, rated as %.2f%%, spent %s\033[0m", c.successfull, c.all, c.failed, c.score, c.duration))
		}
	}
}

func (c *suitConfig) printTestStatus(id int, asId ...int) {
	testCase := c.Cases[id]
	i := id
	if len(asId) > 0 {
		i = asId[0]
	}

	for _, j := range c.getScenarioIds() {
		if j == id {
			if testCase.CanShow() {
				if testCase.IsSuccessful() {
					print(fmt.Sprintf("\033[32m✓ %2d  %s, %s, secret phrase: %s\033[0m", i, testCase.Case, testCase.Duration, testCase.SecretPhrase))
				} else {
					print(fmt.Sprintf("\033[31m✗ %2d  %s, %s\033[0m", i, testCase.Case, testCase.Duration))
				}

				if (verbosity > 1 && testCase.IsFailed()) || (verbosity > 2) {
					for _, name := range testCase.Before {
						log.Printf("(run: %s)\n", name)
						log.Printf(">> script:\n%s\n", strings.TrimSpace(c.Cases[c.getIdByName(name)].Script))
						log.Printf(">> stdout:\n%s\n", c.Cases[c.getIdByName(name)].Stdout)
						if c.Cases[c.getIdByName(name)].Result == nil {
							log.Printf(">> exit status 0 (successfull)")
						} else {
							log.Printf(">> %s (failure)", c.Cases[c.getIdByName(name)].Result)
						}
						log.Printf("---")
					}

					log.Printf("~~~~~")
					log.Printf(">> stdout:\n%s", strings.TrimSpace(testCase.Stdout))
					if testCase.Result == nil {
						log.Printf(">> exit status 0 (successfull)")
					} else {
						log.Printf(">> %s (failure)", testCase.Result)
					}
					log.Printf("~~~~~")

					for _, name := range testCase.After {
						log.Printf("(run: %s)\n", name)
						log.Printf(">> script:\n%s\n", strings.TrimSpace(c.Cases[c.getIdByName(name)].Script))
						log.Printf(">> stdout:\n%s\n", c.Cases[c.getIdByName(name)].Stdout)
						if c.Cases[c.getIdByName(name)].Result == nil {
							log.Printf(">> exit status 0 (successfull)")
						} else {
							log.Printf(">> %s (failure)", c.Cases[c.getIdByName(name)].Result)
						}
						log.Printf("---")
					}
				}
			}
			return
		}
	}
}

func (c *suitConfig) exec(item int) {
	testCase := &c.Cases[item]
	if testCase.Script != "" {
		taskStartTime := time.Now()

		for _, name := range testCase.Before {
			c.Cases[c.getIdByName(name)].RunBash()
		}

		testCase.RunBash()

		for _, name := range testCase.After {
			c.Cases[c.getIdByName(name)].RunBash()
		}

		testCase.Duration = duration(taskStartTime, time.Now())
	}
}

func (t *suitConfig) getConf(config []byte) *suitConfig {
	err := yaml.Unmarshal(config, t)

	if err != nil {
		// TODO don't use Fatal out of main function
		log.Fatal(fmt.Sprintf("Cannot recognize configuration structure in %s file: ", config))
	}

	var envs map[string]string
	wdir, _ := os.Getwd()
	if workdir != "" {
		wdir = workdir
	}

	for i := 0; i < len((*t).Cases); i++ {
		if (*t).Cases[i].GlobalEnv != nil {
			envs = (*t).Cases[i].GlobalEnv
		} else {
			(*t).Cases[i].GlobalEnv = envs
		}

		if (*t).Cases[i].Workdir != "" {
			wdir = (*t).Cases[i].Workdir
		} else {
			(*t).Cases[i].Workdir = wdir
			if wdir == "" {
				wdir, _ = os.Getwd()
			}
		}

		if (*t).Cases[i].Case != "" {
			(*t).Cases[i].canShow = true
			(*t).Cases[i].canRun = true
		}

		if (*t).Cases[i].Name == "" {
			(*t).Cases[i].canRun = true
		}

		if (*t).Cases[i].CanShow() {
			if (*t).Cases[i].Weight == 0 {
				(*t).Cases[i].Weight = 1
			}
		}
	}
	return t
}

func duration(start time.Time, finish time.Time) string {
	return finish.Sub(start).Truncate(time.Millisecond).String()
}

func main() {
	// TODO understand what it is
	wdir := flag.String("working-directory", "", "Specify a working directory")

	// TODO understand what it is
	v1 := flag.Bool("v", false, "Verbosity (mode 1). Show description if it's set")
	// TODO understand what it is
	v2 := flag.Bool("vv", false, "Verbosity (mode 2). Show failed outputs")
	// TODO understand what it is
	v3 := flag.Bool("vvv", false, "Verbosity (mode 3). Show failed and successful outputs")

	flag.Parse()

	// Set Log Level
	// https://golang.org/pkg/log/#example_Logger
	log.SetFlags(0)

	verbosity = func() int {
		if *v1 {
			return 1
		}
		if *v2 {
			return 2
		}
		if *v3 {
			return 3
		}
		return 0
	}()

	workdir = *wdir

	var c suitConfig

	c.getConf(yamlConfig)

	c.printHeader()
	if c.getScenarioCount() > 0 {
		print("-----------------------------------------------------------------------------------")
		i := 1
		for _, id := range c.getScenarioIds() {
			c.exec(id)
			if c.Cases[id].CanShow() {
				c.printTestStatus(id, i)
				i++
			}
		}
		print("-----------------------------------------------------------------------------------")
	}
	c.signOff()
	c.printSummary()
}
