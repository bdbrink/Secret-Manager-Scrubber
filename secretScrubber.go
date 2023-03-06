package main

import (
	"context"
	"fmt"
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

func getSecrets(ctx context.Context, secretsPaginator ListSecretsPag) []string {

	flaggedSecrets := []string{}
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
				log.Infof("is %v gucci gang to delete ?", *secret.Name)
				flaggedSecrets = append(flaggedSecrets, *secret.ARN)
			}
		}
	}
	return flaggedSecrets
}

func deleteSecrets(ctx context.Context, svc *secretsmanager.Client, secrets []string) {

	for _, secret := range secrets {
		log.Infoln(secret)
		deleteParams := &secretsmanager.DeleteSecretInput{
			SecretId: aws.String(secret),
		}
		log.Info(svc.DeleteSecret(context.TODO(), deleteParams))
	}
}

// need to insert channel and slack token
func sendNoti(numberOfSecrets int) {

	api := slack.New("")
	message := fmt.Sprintf("Deleted %v secrets in secrets manager \n", numberOfSecrets)
	slackMsg := slack.Attachment{
		Pretext: "Secrets Scrubber",
		Text:    message,
		Color:   "4af030",
	}
	channelId := ""
	_, timestamp, err := api.PostMessage(channelId, slack.MsgOptionAttachments(slackMsg))

	if err != nil {
		panic(err)
	}
	log.Infof("Message sent succesfully at %s \n", timestamp)
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
