package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	log "github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

type ListSecretsPag interface {
	HasMorePages() bool
	NextPage(ctx context.Context, optFns ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error)
}

// loop through all secrets using the paginator
func getSecrets(ctx context.Context, secretsPaginator ListSecretsPag) []string {

	flaggedSecrets := []string{}
	// two year old secrets, adjust depending on usecases
	twoYearsAgo := time.Now().AddDate(-2, 0, 0)

	for secretsPaginator.HasMorePages() {
		output, err := secretsPaginator.NextPage(ctx)
		if err != nil {
			log.Errorln("error")
		}

		for _, secret := range output.SecretList {
			// check for access date prior to assigning the value as a pointer
			if secret.LastAccessedDate != nil {
				secretLastAcessDate := *secret.LastAccessedDate
				if secretLastAcessDate.Before(twoYearsAgo) {
					log.Infoln(*secret.Name)
					flaggedSecrets = append(flaggedSecrets, *secret.ARN)
				}
			} else {
				// if it is not accessed at all append to slice
				// becareful with secrets that have been accessed, could have been newly created
				// TODO add check for creation date
				log.Infof("%v has not been accessed at all", *secret.Name)
				flaggedSecrets = append(flaggedSecrets, *secret.ARN)
			}
		}
	}
	return flaggedSecrets
}

// all secrets from the flagged secrets slice will be deleted
func deleteSecrets(ctx context.Context, svc *secretsmanager.Client, secrets []string) {

	for _, secret := range secrets {
		log.Infoln(secret)
		deleteParams := &secretsmanager.DeleteSecretInput{
			SecretId:             aws.String(secret),
			RecoveryWindowInDays: aws.Int64(7),
		}
		log.Info(svc.DeleteSecret(context.TODO(), deleteParams))
	}
}

// need to insert channel and slack token
func sendNoti(numberOfSecrets int) bool {

	api := slack.New("")
	message := fmt.Sprintf("Deleted %v secrets in secrets manager \n", numberOfSecrets)
	slackMsg := slack.Attachment{
		Pretext: "Secrets Scrubber",
		Text:    message,
		Color:   "4af030",
	}
	channelID := ""
	_, timestamp, err := api.PostMessage(channelID, slack.MsgOptionAttachments(slackMsg))

	if err != nil {
		panic(err)
	}
	log.Infof("Message sent succesfully at %s \n", timestamp)

	uploadParam := slack.FileUploadParameters{
		File:     "/tmp/flagged.csv",
		Title:    "DeletedSecrets",
		Channels: []string{channelID},
		Filetype: "csv",
	}

	sendIt, err := api.UploadFile(uploadParam)
	if err != nil {
		log.Errorln(err)
		return false
	}
	log.Infoln(sendIt)
	return true
}

func createSecretsFile(secretList []string) string {

	csvFile, err := os.Create("/tmp/flagged.csv")

	if err != nil {
		log.Errorln(err)
		return "file not created"
	}

	csvwriter := csv.NewWriter(csvFile)
	defer csvwriter.Flush()
	csvwriter.Write(secretList)

	return "file created succesfully"
}

func main() {

	//Initialize the session
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRetryer(func() aws.Retryer {
			return retry.AddWithMaxAttempts(
				retry.AddWithMaxBackoffDelay(retry.NewStandard(), 10*time.Second), 10)
		}),
	)
	if err != nil {
		log.Errorln(err)
	}

	svc := secretsmanager.NewFromConfig(cfg)
	listParams := &secretsmanager.ListSecretsInput{}
	secretsPaginator := secretsmanager.NewListSecretsPaginator(svc, listParams, func(o *secretsmanager.ListSecretsPaginatorOptions) {})
	secretList := getSecrets(context.TODO(), secretsPaginator)
	numberOfSecrets := len(secretList)
	log.Infoln(numberOfSecrets)
	deleteSecrets(context.TODO(), svc, secretList)
	sendNoti(numberOfSecrets)

}
