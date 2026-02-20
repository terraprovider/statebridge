package auth

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
	"github.com/pkg/errors"
)

func (c *CredentialConfiguration) AzTables(storageAccountName, tableName string) (*aztables.Client, error) {
	serviceUrl := fmt.Sprintf("https://%s.table.core.windows.net/%s", storageAccountName, tableName)
	creds, err := c.AzCore()
	if err != nil {
		return nil, errors.Wrap(err, "error creating credentials for AzTables client")
	}
	client, err := aztables.NewClient(serviceUrl, creds, nil)
	return client, errors.Wrap(err, "error creating AzTables client")
}
