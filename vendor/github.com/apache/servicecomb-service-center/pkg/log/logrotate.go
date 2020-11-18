/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package log

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var pathReplacer *strings.Replacer

func EscapPath(msg string) string {
	return pathReplacer.Replace(msg)
}

func removeFile(path string) error {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fileInfo.IsDir() {
		return nil
	}
	err = os.Remove(path)
	if err != nil {
		return err
	}
	return nil
}

func removeExceededFiles(path string, baseFileName string,
	maxKeptCount int, rotateStage string) {
	if maxKeptCount < 0 {
		return
	}
	var pat string
	if rotateStage == "rollover" {
		//rotated file, svc.log.20060102150405000
		pat = fmt.Sprintf(`%s\.[0-9]{1,17}$`, baseFileName)
	} else if rotateStage == "backup" {
		//backup compressed file, svc.log.20060102150405000.zip
		pat = fmt.Sprintf(`%s\.[0-9]{17}\.zip$`, baseFileName)
	} else {
		return
	}
	fileList, err := FilterFileList(path, pat, 0777)
	if err != nil {
		Error("filepath.Walk() "+EscapPath(path)+" failed", err)
		return
	}
	sort.Strings(fileList)
	if len(fileList) <= maxKeptCount {
		return
	}
	//remove exceeded files, keep file count below maxBackupCount
	for len(fileList) > maxKeptCount {
		filePath := fileList[0]
		Warn("remove " + EscapPath(filePath))
		err := removeFile(filePath)
		if err != nil {
			Error("remove "+EscapPath(filePath)+" failed", err)
			break
		}
		//remove the first element of a list
		fileList = append(fileList[:0], fileList[1:]...)
	}
}

//filePath: file full path, like ${_APP_LOG_DIR}/svc.log.1
//fileBaseName: rollover file base name, like svc.log
//replaceTimestamp: whether or not to replace the num. of a rolled file
func compressFile(filePath, fileBaseName string, replaceTimestamp bool) error {
	ifp, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer ifp.Close()

	var zipFilePath string
	if replaceTimestamp {
		//svc.log.1 -> svc.log.20060102150405000.zip
		zipFileBase := fileBaseName + "." + getTimeStamp() + "." + "zip"
		zipFilePath = filepath.Dir(filePath) + "/" + zipFileBase
	} else {
		zipFilePath = filePath + ".zip"
	}
	zipFile, err := os.OpenFile(zipFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0440)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	ofp, err := zipWriter.Create(filepath.Base(filePath))
	if err != nil {
		return err
	}

	_, err = io.Copy(ofp, ifp)
	if err != nil {
		return err
	}

	return nil
}

func shouldRollover(fPath string, MaxFileSize int) bool {
	if MaxFileSize <= 0 {
		return false
	}

	fileInfo, err := os.Stat(fPath)
	if err != nil {
		Error("state "+EscapPath(fPath)+" failed", err)
		return false
	}

	if fileInfo.Size() > int64(MaxFileSize*1024*1024) {
		return true
	}
	return false
}

func doRollover(fPath string, MaxFileSize int, MaxBackupCount int) {
	if !shouldRollover(fPath, MaxFileSize) {
		return
	}

	timeStamp := getTimeStamp()
	//absolute path
	rotateFile := fPath + "." + timeStamp
	err := CopyFile(fPath, rotateFile)
	if err != nil {
		Error("copy "+EscapPath(fPath)+" failed", err)
	}

	//truncate the file
	f, err := os.OpenFile(fPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		Error("truncate "+EscapPath(fPath)+" failed", err)
		return
	}
	f.Close()

	//remove exceeded rotate files
	removeExceededFiles(filepath.Dir(fPath), filepath.Base(fPath), MaxBackupCount, "rollover")
}

func doBackup(fPath string, MaxBackupCount int) {
	if MaxBackupCount <= 0 {
		return
	}
	pat := fmt.Sprintf(`%s\.[0-9]{1,17}$`, filepath.Base(fPath))
	rotateFileList, err := FilterFileList(filepath.Dir(fPath), pat, 0777)
	if err != nil {
		Error("walk"+EscapPath(fPath)+" failed", err)
		return
	}

	for _, file := range rotateFileList {
		var err error
		p := fmt.Sprintf(`%s\.[0-9]{17}$`, filepath.Base(fPath))
		if ret, _ := regexp.MatchString(p, file); ret {
			//svc.log.20060102150405000, not replace Timestamp
			err = compressFile(file, filepath.Base(fPath), false)
		} else {
			//svc.log.1, replace Timestamp
			err = compressFile(file, filepath.Base(fPath), true)
		}
		if err != nil {
			Error("compress"+EscapPath(file)+" failed", err)
			continue
		}
		err = removeFile(file)
		if err != nil {
			Error("remove"+EscapPath(file)+" failed", err)
		}
	}

	//remove exceeded backup files
	removeExceededFiles(filepath.Dir(fPath), filepath.Base(fPath), MaxBackupCount, "backup")
}

func RotateFile(file string, MaxFileSize int, MaxBackupCount int) {
	defer func() {
		if e := recover(); e != nil {
			Errorf(nil, "Rotate file %s catch an exception, err: %v.", EscapPath(file), e)
		}
	}()

	doRollover(file, MaxFileSize, MaxBackupCount)
	doBackup(file, MaxBackupCount)
}

//path:			where log files need rollover
//MaxFileSize: 		MaxSize of a file before rotate. By M Bytes.
//MaxBackupCount: 	Max counts to keep of a log's backup files.
func Rotate(path string, MaxFileSize int, MaxBackupCount int) {
	//filter .log .trace files
	defer func() {
		if e := recover(); e != nil {
			Errorf(nil, "Rotate catch an exception, err: %v.", e)
		}
	}()

	pat := `.(\.log|\.trace|\.out)$`
	fileList, err := FilterFileList(path, pat, 0200)
	if err != nil {
		Error("filepath.Walk() "+EscapPath(path)+" failed", err)
		return
	}

	for _, file := range fileList {
		RotateFile(file, MaxFileSize, MaxBackupCount)
	}
}

func isSkip(f os.FileInfo, permits os.FileMode) bool {
	//dir or permission deny
	return f.IsDir() || (f.Mode()&permits == 0000)
}

//path    : where the file will be filtered
//pat     : regexp pattern to filter the matched file
//permit  : check the file whether match any of the permits or not
func FilterFileList(path, pat string, permits os.FileMode) ([]string, error) {
	capacity := 10
	//initialize a fileName slice, len=0, cap=10
	fileList := make([]string, 0, capacity)

	err := filepath.Walk(path,
		func(pathName string, f os.FileInfo, e error) error {
			if f == nil {
				return e
			}
			if isSkip(f, permits) {
				return nil
			}
			if pat != "" {
				ret, _ := regexp.MatchString(pat, f.Name())
				if !ret {
					return nil
				}
			}
			fileList = append(fileList, pathName)
			return nil
		})
	return fileList, err
}

func getTimeStamp() string {
	now := time.Now().Format("2006.01.02.15.04.05.000")
	timeSlot := strings.Replace(now, ".", "", -1)
	return timeSlot
}

func CopyFile(srcFile, destFile string) error {
	file, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer file.Close()

	dest, err := os.OpenFile(destFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer dest.Close()
	_, err = io.Copy(dest, file)
	return err
}

func init() {
	var s []string
	if e := os.Getenv("INSTALL_ROOT"); len(e) > 0 {
		s = append(s, e, "INSTALL_ROOT")
	}
	if e := os.Getenv("SSL_ROOT"); len(e) > 0 {
		s = append(s, e, "SSL_ROOT")
	}
	if e := os.Getenv("CIPHER_ROOT"); len(e) > 0 {
		s = append(s, e, "CIPHER_ROOT")
	}
	if e := os.Getenv("APP_ROOT"); len(e) > 0 {
		s = append(s, e, "APP_ROOT")
	}
	if e := os.Getenv("_APP_LOG_DIR"); len(e) > 0 {
		s = append(s, e, "_APP_LOG_DIR")
	}
	if e := os.Getenv("_APP_SHARE_DIR"); len(e) > 0 {
		s = append(s, e, "_APP_SHARE_DIR")
	}
	if e := os.Getenv("_APP_TMP_DIR"); len(e) > 0 {
		s = append(s, e, "_APP_TMP_DIR")
	}

	pathReplacer = strings.NewReplacer(s...)
}
