/*
 * Copyright (c) 2016-2018 Readium Foundation
 *
 * Redistribution and use in source and binary forms, with or without modification,
 * are permitted provided that the following conditions are met:
 *
 *  1. Redistributions of source code must retain the above copyright notice, this
 *     list of conditions and the following disclaimer.
 *  2. Redistributions in binary form must reproduce the above copyright notice,
 *     this list of conditions and the following disclaimer in the documentation and/or
 *     other materials provided with the distribution.
 *  3. Neither the name of the organization nor the names of its contributors may be
 *     used to endorse or promote products derived from this software without specific
 *     prior written permission
 *
 *  THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
 *  ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
 *  WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
 *  DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR CONTRIBUTORS BE LIABLE FOR
 *  ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 *  (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
 *  LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND
 *  ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 *  (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
 *  SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package ctrl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/gorilla/mux"

	"github.com/jinzhu/gorm"
	"github.com/readium/readium-lcp-server/api"
	"github.com/readium/readium-lcp-server/epub"
	"github.com/readium/readium-lcp-server/pack"
	"github.com/readium/readium-lcp-server/storage"
	"github.com/readium/readium-lcp-server/store"
)

// StoreContent stores content in the storage
// the content name is given in the url (name)
// a temporary file is created, then deleted after the content has been stored
//
func StoreContent(resp http.ResponseWriter, req *http.Request, server IServer) {
	vars := mux.Vars(req)

	size, payload, err := writeRequestFileToTemp(req.Body)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}

	defer cleanupTemp(payload)

	t := pack.NewTask(vars["name"], payload, size)
	result := server.Source().Post(t)

	if result.Error != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: result.Error.Error()}, http.StatusBadRequest)
		return
	}

	// must come *after* w.Header().Add()/Set(), but before w.Write()
	resp.WriteHeader(http.StatusCreated)

	json.NewEncoder(resp).Encode(result.Id)
}

// AddContent adds content to the storage
// lcp spec : store data resulting from an external encryption
// PUT method with PAYLOAD : LcpPublication in json format
// content_id is also present in the url.
// if contentID is different , url key overrides the content id in the json payload
// this method adds the <protected_content_location>  in the store (of encrypted files)
// and the key in the database in order to create the licenses
func AddContent(resp http.ResponseWriter, req *http.Request, server IServer) {
	// parse the json payload
	vars := mux.Vars(req)
	decoder := json.NewDecoder(req.Body)
	var publication api.LcpPublication
	err := decoder.Decode(&publication)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
	// get the content ID in the url
	contentID := vars["content_id"]
	if contentID == "" {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: "The content id must be set in the url"}, http.StatusBadRequest)
		return
	}
	// open the encrypted file from the path given in the json payload
	file, err := os.Open(publication.Output)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// TODO : shouldn't be this the last step, after operating database?
	// add the file to the storage, named from contentID
	_, err = server.Storage().Add(contentID, file)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}

	var content *store.Content
	// insert row in database if key does not exist
	content, err = server.Store().Content().Get(contentID)
	content.EncryptionKey = publication.ContentKey
	// default values
	content.Location = ""
	content.Length = -1
	content.Sha256 = ""
	if publication.ContentDisposition != nil {
		content.Location = *publication.ContentDisposition
	}

	if publication.Size != nil {
		content.Length = *publication.Size
	}

	if publication.Checksum != nil {
		content.Sha256 = *publication.Checksum
	}

	//todo? check hash & length
	code := http.StatusCreated
	if err == gorm.ErrRecordNotFound { //insert into database
		content.Id = contentID
		err = server.Store().Content().Add(content)
	} else { //update encryption key for content.Id = publication.ContentId
		err = server.Store().Content().Update(content)
		code = http.StatusOK
	}
	if err != nil { //db not updated
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		return
	}

	// must come *after* w.Header().Add()/Set(), but before w.Write()
	resp.WriteHeader(code)

	//json.NewEncoder(w).Encode(publication.ContentId)

}

// ListContents lists the content in the storage index
//
func ListContents(resp http.ResponseWriter, req *http.Request, server IServer) {
	fmt.Fprintf(os.Stderr, "Listing contents.")
	contents, err := server.Store().Content().List()
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		return
	}

	resp.Header().Set(api.HdrContentType, api.ContentTypeJson)
	enc := json.NewEncoder(resp)
	err = enc.Encode(contents)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}

}

// GetContent fetches and returns an encrypted content file
// selected by it content id (uuid)
//
func GetContent(resp http.ResponseWriter, req *http.Request, server IServer) {
	// get the content id from the calling url
	vars := mux.Vars(req)
	contentID := vars["content_id"]
	content, err := server.Store().Content().Get(contentID)
	if err != nil { //item probably not found
		if err == gorm.ErrRecordNotFound {
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
		} else {
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		}
		return
	}
	// check the existence of the file
	item, err := server.Storage().Get(contentID)
	if err != nil { //item probably not found
		if err == storage.ErrNotFound {
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
		} else {
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		}
		return
	}
	// opens the file
	contentReadCloser, err := item.Contents()
	defer contentReadCloser.Close()
	if err != nil { //file probably not found
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
	// set headers
	resp.Header().Set(api.HdrContentDisposition, "attachment; filename="+content.Location)
	resp.Header().Set(api.HdrContentType, epub.ContentTypeEpub)
	resp.Header().Set("Content-Length", fmt.Sprintf("%d", content.Length))

	// TODO : no error checking ? no verification if that file exists ?
	// returns the content of the file to the caller
	io.Copy(resp, contentReadCloser)
}
