// Copyright 2021 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/casdoor/casdoor/conf"
	"github.com/casdoor/casdoor/i18n"
	"github.com/casdoor/casdoor/storage"
	"github.com/casdoor/casdoor/util"
	"github.com/casdoor/oss"
)

var isCloudIntranet bool

func init() {
	isCloudIntranet = conf.GetConfigBool("isCloudIntranet")
}

func getProviderEndpoint(provider *Provider) string {
	endpoint := provider.Endpoint
	if provider.IntranetEndpoint != "" && isCloudIntranet {
		endpoint = provider.IntranetEndpoint
	}
	return endpoint
}

func escapePath(path string) string {
	tokens := strings.Split(path, "/")
	if len(tokens) > 0 {
		tokens[len(tokens)-1] = url.QueryEscape(tokens[len(tokens)-1])
	}

	res := strings.Join(tokens, "/")
	return res
}

func GetTruncatedPath(provider *Provider, fullFilePath string, limit int) string {
	pathPrefix := util.UrlJoin(util.GetUrlPath(provider.Domain), provider.PathPrefix)

	dir, file := filepath.Split(fullFilePath)
	ext := filepath.Ext(file)
	fileName := strings.TrimSuffix(file, ext)
	for {
		escapedString := escapePath(escapePath(fullFilePath))
		if len(escapedString) < limit-len(pathPrefix) {
			break
		}
		rs := []rune(fileName)
		fileName = string(rs[0 : len(rs)-1])
		fullFilePath = dir + fileName + ext
	}

	return fullFilePath
}

func GetUploadFileUrl(provider *Provider, fullFilePath string, hasTimestamp bool) (string, string) {
	escapedPath := util.UrlJoin(provider.PathPrefix, fullFilePath)
	objectKey := util.UrlJoin(util.GetUrlPath(provider.Domain), escapedPath)

	host := ""
	if provider.Type != "Local File System" {
		// provider.Domain = "https://cdn.casbin.com/casdoor/"
		// host = util.GetUrlHost(provider.Domain) // bug fix: image path wrong
		host = provider.Domain
		if !strings.HasPrefix(host, "http://") && !strings.HasPrefix(host, "https://") {
			host = fmt.Sprintf("https://%s", host)
		}
	} else {
		// provider.Domain = "http://localhost:8000" or "https://door.casdoor.com"
		host = util.UrlJoin(provider.Domain, "/files")
	}
	if provider.Type == "Azure Blob" {
		host = util.UrlJoin(host, provider.Bucket)
	}

	fileUrl := util.UrlJoin(host, escapePath(objectKey))

	if hasTimestamp {
		fileUrl = fmt.Sprintf("%s?t=%s", fileUrl, util.GetCurrentUnixTime())
	}

	if provider.Type == "Tencent Cloud COS" {
		objectKey = escapePath(objectKey)
	}

	return fileUrl, objectKey
}

func getStorageProvider(provider *Provider, lang string) (oss.StorageInterface, error) {
	endpoint := getProviderEndpoint(provider)
	storageProvider := storage.GetStorageProvider(provider.Type, provider.ClientId, provider.ClientSecret, provider.RegionId, provider.Bucket, endpoint)
	if storageProvider == nil {
		return nil, fmt.Errorf(i18n.Translate(lang, "storage:The provider type: %s is not supported"), provider.Type)
	}

	if provider.Domain == "" {
		provider.Domain = storageProvider.GetEndpoint()
		_, err := UpdateProvider(provider.GetId(), provider)
		if err != nil {
			return nil, err
		}
	}

	return storageProvider, nil
}

func uploadFile(provider *Provider, fullFilePath string, fileBuffer *bytes.Buffer, lang string) (string, string, error) {
	storageProvider, err := getStorageProvider(provider, lang)
	if err != nil {
		return "", "", err
	}

	fileUrl, objectKey := GetUploadFileUrl(provider, fullFilePath, true)

	objectKeyRefined := objectKey
	if provider.Type == "Google Cloud Storage" {
		objectKeyRefined = strings.TrimPrefix(objectKeyRefined, "/")
	}

	_, err = storageProvider.Put(objectKeyRefined, fileBuffer)
	if err != nil {
		return "", "", err
	}

	return fileUrl, objectKey, nil
}

func UploadFileSafe(provider *Provider, fullFilePath string, fileBuffer *bytes.Buffer, lang string) (string, string, error) {
	// check fullFilePath is there security issue
	if strings.Contains(fullFilePath, "..") {
		return "", "", fmt.Errorf("the fullFilePath: %s is not allowed", fullFilePath)
	}

	var fileUrl string
	var objectKey string
	var err error
	times := 0
	for {
		fileUrl, objectKey, err = uploadFile(provider, fullFilePath, fileBuffer, lang)
		if err != nil {
			times += 1
			if times >= 5 {
				return "", "", err
			}
		} else {
			break
		}
	}
	return fileUrl, objectKey, nil
}

func DeleteFile(provider *Provider, objectKey string, lang string) error {
	// check fullFilePath is there security issue
	if strings.Contains(objectKey, "..") {
		return fmt.Errorf(i18n.Translate(lang, "storage:The objectKey: %s is not allowed"), objectKey)
	}

	storageProvider, err := getStorageProvider(provider, lang)
	if err != nil {
		return err
	}

	return storageProvider.Delete(objectKey)
}
