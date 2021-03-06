package main

import (
	"bytes"
	"fmt"
	"github.com/abadojack/whatlanggo"
	"github.com/andygrunwald/go-trending"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/eawsy/aws-lambda-go-core/service/lambda/runtime"
	"github.com/eawsy/aws-lambda-go-event/service/lambda/runtime/event/cloudwatchschedevt"
	"github.com/gorilla/feeds"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
	"strings"
	"time"
)

var log = logging.MustGetLogger("github-trending")

const authorName string = "Jacek Szubert"
const authorEmail string = "jacek.szubert@gmail.com"

//Filter out nautral languages by ISO 639-3 code
var naturalLanguagesToExclude = []string{"cmn", "kor"}

type project struct {
	name        string
	language    string
	description string
	url         string
	author      string
	authorURL   string
}

func parseS3Path(s3Path string) (string, string) {
	s3PathWithoutPrefix := strings.Replace(s3Path, "s3://", "", 1)
	s3Bucket := strings.Split(s3PathWithoutPrefix, "/")[0]
	s3Path = strings.Join(strings.Split(s3PathWithoutPrefix, "/")[1:], "/")
	return s3Bucket, s3Path
}

func getDataFromS3(s3FullPath string) string {
	s3Bucket, s3Path := parseS3Path(s3FullPath)

	s3svc := s3.New(session.New())
	input := &s3.GetObjectInput{
		Bucket: aws.String(s3Bucket),
		Key:    aws.String(s3Path),
	}
	result, err := s3svc.GetObject(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() != s3.ErrCodeNoSuchKey {
				log.Fatal(aerr.Error())
			}
		}
	}

	if result.Body == nil {
		return ""
	}
	body, err := ioutil.ReadAll(result.Body)
	if err != nil {
		log.Fatal(err)
	}
	return string(body)
}

func putDataToS3(s3FullPath string, data string) {
	s3Bucket, s3Path := parseS3Path(s3FullPath)

	s3svc := s3.New(session.New())
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s3Bucket),
		Key:         aws.String(s3Path),
		GrantRead:   aws.String("uri=http://acs.amazonaws.com/groups/global/AllUsers"),
		ContentType: aws.String("application/xml"),
		Body:        bytes.NewReader([]byte(data)),
	}
	_, err := s3svc.PutObject(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			log.Fatal(aerr.Error())
		}
	}
}

func stringInArray(str string, array []string) bool {
	for _, arrayElement := range array {
		if arrayElement == str {
			return true
		}
	}
	return false
}

func recognizeNaturalLanguage(text string) string {
	languageInfo := whatlanggo.Detect(text)
	return whatlanggo.LangToString(languageInfo.Lang)
}

func generateFeed(projects []project) *feeds.Feed {
	now := time.Now().UTC()
	feed := &feeds.Feed{
		Title:       "github today's trending",
		Link:        &feeds.Link{Href: "https://github.com/trending"},
		Description: "Github trending feed",
		Author:      &feeds.Author{Name: authorName, Email: authorEmail},
		Created:     now,
	}

	feed.Items = []*feeds.Item{}
	var title string
	var description string
	for _, project := range projects {
		if len(project.description) > 0 {
			description = project.description
		} else {
			description = project.name
		}
		if len(project.language) > 0 {
			title = fmt.Sprintf("%s [%s]", description, project.language)
		} else {
			title = project.description
		}

		item := &feeds.Item{
			Title:   title,
			Link:    &feeds.Link{Href: project.url},
			Author:  &feeds.Author{Name: project.author, Email: project.authorURL},
			Created: now,
		}
		feed.Items = append(feed.Items, item)
	}
	return feed
}

func Handle(evt *cloudwatchschedevt.Event, ctx *runtime.Context) (interface{}, error) {
	var viewedProjectsS3Path string = "s3://" + os.Getenv("S3_BUCKET") + "/github_trending_all_daily"

	trend := trending.NewTrending()
	viewedProjects := strings.Split(getDataFromS3(viewedProjectsS3Path), "\n")
	newProjects := []project{}

	projects, err := trend.GetProjects(trending.TimeToday, "all")
	if err != nil {
		log.Fatal(err)
	}
	var author string
	var authorURL string
	var naturalLanguage string
	for _, proj := range projects {
		proj.Name = strings.Replace(proj.Name, " ", "", -1)
		naturalLanguage = recognizeNaturalLanguage(proj.Description)
		if !stringInArray(proj.Name, viewedProjects) && !stringInArray(naturalLanguage, naturalLanguagesToExclude) {
			viewedProjects = append(viewedProjects, proj.Name)
			if len(proj.Contributer) > 0 {
				author = proj.Contributer[0].DisplayName
				authorURL = proj.Contributer[0].URL.String()
			} else {
				author = "NaN"
				authorURL = "NaN"
			}
			newProjects = append(
				newProjects,
				project{
					name:        proj.Name,
					language:    proj.Language,
					description: proj.Description,
					url:         proj.URL.String(),
					author:      author,
					authorURL:   authorURL,
				},
			)
		}
	}

	feed := generateFeed(newProjects)
	atom, err := feed.ToAtom()
	if err != nil {
		log.Fatal(err)
	}
	putDataToS3(viewedProjectsS3Path+".atom", atom)
	rss, err := feed.ToRss()
	if err != nil {
		log.Fatal(err)
	}
	putDataToS3(viewedProjectsS3Path+".rss", rss)

	putDataToS3(viewedProjectsS3Path, strings.Join(viewedProjects, "\n"))

	return nil, nil
}

func main() {}
