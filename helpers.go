package cobraprompt

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/c-bata/go-prompt"
	"github.com/hashicorp/consul/api"
)

func GetPlatformId(c *api.Client) []prompt.Suggest {

	filterName := fmt.Sprintf("Meta.app == \"platform\"")
	nodes, _, err := c.Catalog().Nodes(&api.QueryOptions{Filter: filterName})
	if err != nil {
		fmt.Println(err)
	}

	s := make([]prompt.Suggest, len(nodes))
	for i := range nodes {
		s[i] = prompt.Suggest{
			Text:        nodes[i].Meta["provider_id"],
			Description: fmt.Sprintf("(%s)", nodes[i].Meta["customer_name"]),
		}
	}
	return s
}

func GetPlatformNames(c *api.Client) []prompt.Suggest {

	filterName := fmt.Sprintf("Meta.app == \"platform\"")
	nodes, _, err := c.Catalog().Nodes(&api.QueryOptions{Filter: filterName})
	if err != nil {
		fmt.Println(err)
	}

	s := make([]prompt.Suggest, len(nodes))
	for i := range nodes {
		s[i] = prompt.Suggest{
			Text:        nodes[i].Meta["customer_name"],
			Description: fmt.Sprintf("(%s)", nodes[i].Meta["provider_id"]),
		}
	}
	return s
}

func getPreviousOption(d prompt.Document) (cmd, option string, found bool) {
	args := strings.Split(d.TextBeforeCursor(), " ")
	l := len(args)
	if l >= 2 {
		option = args[l-2]
	}
	if strings.HasPrefix(option, "-") {
		return args[0], option, true
	}
	return "", "", false
}

func checkProfile(d prompt.Document) (string, bool) {
	args := strings.Split(d.TextBeforeCursor(), " ")
	profiles, _ := FindProfile()

	for _, opts := range args {
		if contains(profiles, opts) {
			return opts, true
		}
	}
	return "", false
}

func completeOptionArguments(d prompt.Document, co CobraPrompt) ([]prompt.Suggest, bool) {
	var client *api.Client
	_, option, found := getPreviousOption(d)
	if !found {
		return []prompt.Suggest{}, false
	}

	_, suggest := FindProfile()
	entry, prev := checkProfile(d)

	if option == "-id" || option == "--id" {
		if prev {
			token, env := GetEnv(entry)
			client = ConsulInit(token, env, entry)
			return prompt.FilterFuzzy(
				GetPlatformId(client),
				d.GetWordBeforeCursor(),
				true,
			), true
		}
		return prompt.FilterFuzzy(
			GetPlatformId(co.Consul),
			d.GetWordBeforeCursor(),
			true,
		), true

	}

	if option == "-name" || option == "--name" {
		if prev {
			token, env := GetEnv(entry)
			client = ConsulInit(token, env, entry)
			return prompt.FilterFuzzy(
				GetPlatformNames(client),
				d.GetWordBeforeCursor(),
				true,
			), true
		}
		return prompt.FilterFuzzy(
			GetPlatformNames(co.Consul),
			d.GetWordBeforeCursor(),
			true,
		), true

	}

	if option == "-profile" || option == "--profile" {
		return prompt.FilterFuzzy(
			suggest,
			d.GetWordBeforeCursor(),
			true,
		), true

	}

	return []prompt.Suggest{}, false
}

// checks if a file exists
func FileExists(filename string) bool {
	_, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return true
}

// find the profiles on the existing machine
func FindProfile() ([]string, []prompt.Suggest) {
	var suggestions []prompt.Suggest
	var profiles []string
	dirname, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	config := fmt.Sprintf("%s/.aws/config", dirname)
	creds := fmt.Sprintf("%s/.aws/credentails", dirname)

	if FileExists(config) {
		file, err := os.Open(config)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		// optionally, resize scanner's capacity for lines over 64K, see next example
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "[") {
				t := strings.Replace(scanner.Text(), "[", "", -1)
				t = strings.Replace(t, "]", "", -1)
				t = strings.Replace(t, "profile ", "", -1)
				profiles = append(profiles, t)
				suggestions = append(suggestions, prompt.Suggest{Text: t, Description: ""})
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}
	if FileExists(creds) {
		file, err := os.Open(creds)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		// optionally, resize scanner's capacity for lines over 64K, see next example
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "[") {
				t := strings.Replace(scanner.Text(), "[", "", -1)
				t = strings.Replace(t, "]", "", -1)
				profiles = append(profiles, t)
				suggestions = append(suggestions, prompt.Suggest{Text: t, Description: ""})
			}
		}
		if err := scanner.Err(); err != nil {
			log.Fatal(err)
		}
	}
	return profiles, suggestions
}

func GetSecret(dc string, profile string) (string, error) {
	secretName := fmt.Sprintf("%s/consul", dc)
	//Create a Secrets Manager client
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            aws.Config{Region: aws.String("us-west-2")},
		Profile:           profile,
	}))
	svc := secretsmanager.New(sess)
	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(secretName),
		VersionStage: aws.String("AWSCURRENT"), // VersionStage defaults to AWSCURRENT if unspecified
	}
	result, err := svc.GetSecretValue(input)
	if err != nil {
		return "", err
	}

	// Decrypts secret using the associated KMS CMK.
	// Depending on whether the secret is a string or binary, one of these fields will be populated.
	var secretString string
	if result.SecretString != nil {
		secretString = *result.SecretString
	}
	return secretString, err
	// Your code goes here.
}

func GetEnv(profile string) (string, string) {
	token, err := GetSecret("mm-eng", profile)
	if err != nil {
		token, err := GetSecret("mm-prod", profile)
		if err != nil {
			os.Exit(1)
		}
		return token, "prod"
	}
	return token, "eng"
}

func ConsulInit(token string, env string, profile string) *api.Client {
	address := fmt.Sprintf("http://consul-%s.mixmode.ai", env)
	client, err := api.NewClient(&api.Config{Token: token, Address: address})
	if err != nil {
		panic(err)
	}
	return client
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}
