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
	"log"
	"net/http"
	"strconv"

	"fmt"
	"github.com/Machiel/slugify"
	"github.com/gorilla/mux"
	"github.com/jinzhu/gorm"
	"github.com/readium/readium-lcp-server/api"
	"github.com/readium/readium-lcp-server/store"
	"io"
	"io/ioutil"
	"os"
	"path"
)

// GetPublications returns a list of publications
func GetPublications(resp http.ResponseWriter, req *http.Request, server IServer) {
	var page int64
	var perPage int64
	var err error

	if req.FormValue("page") != "" {
		page, err = strconv.ParseInt((req).FormValue("page"), 10, 32)
		if err != nil {
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
			return
		}
	} else {
		page = 1
	}

	if req.FormValue("per_page") != "" {
		perPage, err = strconv.ParseInt((req).FormValue("per_page"), 10, 32)
		if err != nil {
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
			return
		}
	} else {
		perPage = 30
	}

	if page > 0 {
		page-- //pagenum starting at 0 in code, but user interface starting at 1
	}

	if page < 0 {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: "page must be positive integer"}, http.StatusBadRequest)
		return
	}

	pubs, err := server.Store().Publication().List(int(perPage), int(page))
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		return
	}

	if len(pubs) > 0 {
		nextPage := strconv.Itoa(int(page) + 1)
		resp.Header().Set("Link", "</publications/?page="+nextPage+">; rel=\"next\"; title=\"next\"")
	}
	if page > 1 {
		previousPage := strconv.Itoa(int(page) - 1)
		resp.Header().Set("Link", "</publications/?page="+previousPage+">; rel=\"previous\"; title=\"previous\"")
	}
	resp.Header().Set(api.HdrContentType, api.ContentTypeJson)

	enc := json.NewEncoder(resp)
	err = enc.Encode(pubs)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
}

// GetPublication returns a publication from its numeric id, given as part of the calling url
//
func GetPublication(resp http.ResponseWriter, req *http.Request, server IServer) {
	vars := mux.Vars(req)
	var id int
	var err error
	if id, err = strconv.Atoi(vars["id"]); err != nil {
		// id is not a number
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: "The publication id must be an integer"}, http.StatusBadRequest)
	}

	if pub, err := server.Store().Publication().Get(int64(id)); err == nil {
		enc := json.NewEncoder(resp)
		if err = enc.Encode(pub); err == nil {
			// send json of correctly encoded user info
			resp.Header().Set(api.HdrContentType, api.ContentTypeJson)
			resp.WriteHeader(http.StatusOK)
			return
		}
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
	} else {
		switch err {
		case gorm.ErrRecordNotFound:
			{
				api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
			}
		default:
			{
				api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
			}
		}
	}
}

// CheckPublicationByTitle check if a publication with this title exist
func CheckPublicationByTitle(resp http.ResponseWriter, req *http.Request, server IServer) {
	var title string
	title = req.URL.Query()["title"][0]

	log.Println("Check publication stored with name " + string(title))

	if pub, err := server.Store().Publication().CheckByTitle(string(title)); err == nil {
		enc := json.NewEncoder(resp)
		if err = enc.Encode(pub); err == nil {
			// send json of correctly encoded user info
			resp.Header().Set(api.HdrContentType, api.ContentTypeJson)
			resp.WriteHeader(http.StatusOK)
			return
		}
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
	} else {
		switch err {
		case gorm.ErrRecordNotFound:
			{
				log.Println("No publication stored with name " + string(title))
				//	api.Error(w, r, s.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
			}
		default:
			{
				api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
			}
		}
	}
}

// CreatePublication creates a publication in the database
func CreatePublication(resp http.ResponseWriter, req *http.Request, server IServer) {
	pub, err := api.ReadPublicationPayload(req)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: "incorrect JSON Publication " + err.Error()}, http.StatusBadRequest)
		return
	}

	// get the path to the master file
	inputPath := path.Join(server.Config().FrontendServer.MasterRepository, pub.MasterFilename)

	if _, err := os.Stat(inputPath); err != nil {
		// the master file does not exist
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
		return
	}

	contentDisposition := slugify.Slugify(pub.Title)
	// encrypt the EPUB File and send the content to the LCP server
	err = EncryptEPUB(inputPath, contentDisposition, server)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		return
	}

	// add publication
	if err = server.Store().Publication().Add(pub); err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}

	// publication added to db
	resp.WriteHeader(http.StatusCreated)
}

// UploadEPUB creates a new EPUB file, namd after a file form parameter.
// a temp file is created then deleted.
//UploadEPUB creates a new EPUB file
func UploadEPUB(resp http.ResponseWriter, req *http.Request, server IServer) {
	//var pub store.Publication
	contentDisposition := slugify.Slugify(req.URL.Query()["title"][0])

	file, header, err := req.FormFile("file")

	tmpfile, err := ioutil.TempFile("", "example")

	if err != nil {
		fmt.Fprintln(resp, err)
		return
	}

	defer os.Remove(tmpfile.Name())

	_, err = io.Copy(tmpfile, file)

	if err = tmpfile.Close(); err != nil {
		log.Fatal(err)
	}
	// encrypt the EPUB File and send the content to the LCP server
	if err = EncryptEPUB(tmpfile.Name(), contentDisposition, server); err != nil {
		log.Fatal(err)
	}

	fmt.Fprintf(resp, "File uploaded successfully : ")
	fmt.Fprintf(resp, header.Filename)

}

// UpdatePublication updates an identified publication (id) in the database
func UpdatePublication(resp http.ResponseWriter, req *http.Request, server IServer) {
	vars := mux.Vars(req)
	id, err := strconv.Atoi(vars["id"])
	if err != nil {
		// id is not a number
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: "Plublication ID must be an integer"}, http.StatusBadRequest)
		return
	}
	pub, err := api.ReadPublicationPayload(req)
	// ID is a number, check publication (json)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}
	// publication ok, id is a number, search publication to update
	if foundPub, err := server.Store().Publication().Get(int64(id)); err != nil {
		switch err {
		case gorm.ErrRecordNotFound:
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
		default:
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		}
	} else {
		// publication is found!
		if err := server.Store().Publication().Update(&store.Publication{
			ID:     foundPub.ID,
			Title:  pub.Title,
			Status: foundPub.Status}); err != nil {
			//update failed!
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusInternalServerError)
		}
		//database update ok
		resp.WriteHeader(http.StatusOK)
		//return
	}
}

// DeletePublication removes a publication in the database
func DeletePublication(resp http.ResponseWriter, req *http.Request, server IServer) {
	vars := mux.Vars(req)
	id, err := strconv.ParseInt(vars["id"], 10, 64)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}

	publication, err := server.Store().Publication().Get(id)
	if err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
		return
	}

	// delete the epub file from the master repository
	inputPath := path.Join(server.Config().FrontendServer.MasterRepository, publication.Title+".epub")

	if _, err := os.Stat(inputPath); err == nil {
		err = os.Remove(inputPath)
		if err != nil {
			api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusNotFound)
			return
		}
	}

	if err = server.Store().Publication().Delete(id); err != nil {
		api.Error(resp, req, server.DefaultSrvLang(), api.Problem{Detail: err.Error()}, http.StatusBadRequest)
		return
	}

	// publication deleted from db
	resp.WriteHeader(http.StatusOK)
}
