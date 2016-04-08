// Structures used in Group Store
//  File System
//  /acct/(uuid)/fs  "(uuid)"    { "id": "uuid", "name": "name", "status": "active",
//                                "createdate": <timestamp>, "deletedate": <timestamp>
//                               }
//
// IP Address
// /acct/(uuid)/fs/(uuid)/addr "(uuid)"   { "id": uuid, "addr": "111.111.111.111", "status": "active",
//                                         "createdate": <timestamp>, "deletedate": <timestamp>
//                                       }

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gholt/brimtime"
	"github.com/gholt/store"
	fb "github.com/letterj/oohhc/proto/filesystem"
	"github.com/satori/go.uuid"
	"github.com/spaolacci/murmur3"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
)

var errf = grpc.Errorf

// AcctPayLoad ... Account PayLoad
type AcctPayLoad struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Token      string `json:"token"`
	Status     string `json:"status"`
	CreateDate int64  `json:"createdate"`
	DeleteDate int64  `json:"deletedate"`
}

// FileSysPayLoad ... File System PayLoad
type FileSysPayLoad struct {
	ID          string   `json:"id"`
	AcctID      string   `json:"acctid"`
	Name        string   `json:"name"`
	SizeInBytes int64    `json:"sizeinbytes"`
	Status      string   `json:"status"`
	CreateDate  int64    `json:"createdate"`
	DeleteDate  int64    `json:"deletedate"`
	Addr        []string `json:"addrs"`
}

// AcctRefPayload ...
type AcctRefPayload struct {
	ID     string `json:"id"`
	AcctID string `json:"acctid"`
}

// AddrPayLoad ... IP Address Address PayLoad
type AddrPayLoad struct {
	FSid string `json:"fsid"`
	Addr string `json:"addr"`
}

// FileSystemAPIServer is used to implement oohhc
type FileSystemAPIServer struct {
	fsws *FileSystemWS
}

// NewFileSystemAPIServer ...
func NewFileSystemAPIServer(filesysWS *FileSystemWS) *FileSystemAPIServer {
	s := new(FileSystemAPIServer)
	s.fsws = filesysWS
	return s
}

// CreateFS ...
func (s *FileSystemAPIServer) CreateFS(ctx context.Context, r *fb.CreateFSRequest) (*fb.CreateFSResponse, error) {
	var status string
	var result string
	var acctData AcctPayLoad
	var newFS FileSysPayLoad
	var acctRef AcctRefPayload
	var acctRefB []byte
	var err error
	var dataB []byte
	// Get incomming ip
	pr, ok := peer.FromContext(ctx)
	if ok {
		fmt.Println(pr.Addr)
	}
	// getAcct data
	acctData, err = s.getAcct("/acct", r.Acctnum)
	if err != nil {
		log.Printf("Error %v on lookup for account %s", err, r.Acctnum)
		return nil, err
	}
	// validate token
	if acctData.Token != r.Token {
		return nil, errf(codes.PermissionDenied, "%s", "Invalid Token")
	}
	// Check for to see if file system name exists
	fs := fmt.Sprintf("/acct/%s/fs", acctData.ID)
	err = s.dupNameCheck(fs, r.FSName)
	if err != nil {
		log.Printf("Precondition Failed: %v\n...", err)
		return nil, errf(codes.FailedPrecondition, "%v", err)
	}
	// File system values
	parentKey := fmt.Sprintf("/acct/%s/fs", r.Acctnum)
	childKey := uuid.NewV4().String()
	newFS.ID = childKey
	newFS.AcctID = r.Acctnum
	newFS.Name = r.FSName
	newFS.SizeInBytes = 107374182400
	newFS.Status = "active"
	newFS.CreateDate = time.Now().Unix()
	newFS.DeleteDate = 0
	//write file system
	dataB, err = json.Marshal(newFS)
	if err != nil {
		log.Printf("Marshal Error: %v\n...", err)
		return nil, errf(codes.Internal, "%v", err)
	}
	_, err = s.fsws.writeGStore(parentKey, childKey, dataB)
	if err != nil {
		log.Printf("Write Error: %v", err)
		return nil, errf(codes.Internal, "%v", err)
	}
	// Write special filesystem look up entry
	// "/fs"	"[file system uuid]"		{"id": "[filesystem uuid]", "acctid": "[account uuid]"}
	parentKeyA, parentKeyB := murmur3.Sum128([]byte("/fs"))
	childKeyA, childKeyB := murmur3.Sum128([]byte(newFS.ID))
	timestampMicro := brimtime.TimeToUnixMicro(time.Now())
	acctRef.ID = newFS.ID
	acctRef.AcctID = r.Acctnum
	acctRefB, err = json.Marshal(acctRef)
	if err != nil {
		log.Printf("Marshal Error: %v\n...", err)
		return nil, errf(codes.Internal, "%v", err)
	}
	_, err = s.fsws.gstore.Write(context.Background(), parentKeyA, parentKeyB, childKeyA, childKeyB, timestampMicro, acctRefB)
	if err != nil {
		log.Printf("Write Error: %v", err)
		return nil, errf(codes.Internal, "%v", err)
	}
	// Prep reults to return
	status = "OK"
	result = fmt.Sprintf("File System %s was created from Account %s", childKey, r.Acctnum)
	return &fb.CreateFSResponse{Payload: result, Status: status}, nil
}

// ListFS ...
func (s *FileSystemAPIServer) ListFS(ctx context.Context, r *fb.ListFSRequest) (*fb.ListFSResponse, error) {
	var status string
	var acctData AcctPayLoad
	var err error
	var data string
	// Get incomming ip
	pr, ok := peer.FromContext(ctx)
	if ok {
		fmt.Println(pr.Addr)
	}
	// getAcct data
	acctData, err = s.getAcct("/acct", r.Acctnum)
	if err != nil {
		log.Printf("Error %v on lookup for account %s", err, r.Acctnum)
		return nil, err
	}
	// validate token
	if acctData.Token != r.Token {
		return nil, errf(codes.PermissionDenied, "%s", "Invalid Token")
	}
	// Setup Account read for list of file systems
	parentKey := fmt.Sprintf("/acct/%s/fs", r.Acctnum)
	data, err = s.fsws.readGroupGStore(parentKey)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	// Prep things to return
	status = "OK"
	return &fb.ListFSResponse{Payload: data, Status: status}, nil
}

// ShowFS ...
func (s *FileSystemAPIServer) ShowFS(ctx context.Context, r *fb.ShowFSRequest) (*fb.ShowFSResponse, error) {
	var status string
	var acctData AcctPayLoad
	var fsData FileSysPayLoad
	var fsDataB []byte
	var err error
	// Get incomming ip
	pr, ok := peer.FromContext(ctx)
	if ok {
		fmt.Println(pr.Addr)
	}
	// getAcct data
	acctData, err = s.getAcct("/acct", r.Acctnum)
	if err != nil {
		log.Printf("Error %v on lookup for account %s", err, r.Acctnum)
		return nil, err
	}
	// validate token
	if acctData.Token != r.Token {
		return nil, errf(codes.PermissionDenied, "%s", "Invalid Token")
	}

	pKey := fmt.Sprintf("/acct/%s/fs", r.Acctnum)
	pKeyA, pKeyB := murmur3.Sum128([]byte(pKey))
	cKeyA, cKeyB := murmur3.Sum128([]byte(r.FSid))
	_, fsDataB, err = s.fsws.gstore.Read(context.Background(), pKeyA, pKeyB, cKeyA, cKeyB, nil)
	if store.IsNotFound(err) {
		return nil, errf(codes.NotFound, "%v", "File System Not Found")
	}
	if err != nil {
		return nil, errf(codes.Internal, "%s", err)
	}
	err = json.Unmarshal(fsDataB, &fsData)
	if err != nil {
		return nil, errf(codes.Internal, "%s", err)
	}
	// Get list of Addr
	fsData.Addr, err = s.addrList(fsData.ID)
	if err != nil {
		return nil, errf(codes.Internal, "%s", err)
	}
	fsDataB, err = json.Marshal(&fsData)
	if err != nil {
		return nil, errf(codes.Internal, "%s", err)
	}

	// Prep things to return
	status = "OK"
	return &fb.ShowFSResponse{Payload: string(fsDataB), Status: status}, nil
}

// DeleteFS ...
func (s *FileSystemAPIServer) DeleteFS(ctx context.Context, r *fb.DeleteFSRequest) (*fb.DeleteFSResponse, error) {
	var status string
	var result string
	var dataS string
	var dataB []byte
	var acctData AcctPayLoad
	var fsdata FileSysPayLoad
	var err error
	// Get incomming ip
	pr, ok := peer.FromContext(ctx)
	if ok {
		fmt.Println(pr.Addr)
	}
	// getAcct data
	acctData, err = s.getAcct("/acct", r.Acctnum)
	if err != nil {
		log.Printf("Error %v on lookup for account %s", err, r.Acctnum)
		return nil, err
	}
	// validate token
	if acctData.Token != r.Token {
		return nil, errf(codes.PermissionDenied, "%s", "Invalid Token")
	}
	// Setup keys and get filesystem data
	parentKey := fmt.Sprintf("/acct/%s/fs", r.Acctnum)
	childKey := r.FSid
	dataS, err = s.fsws.getGStore(parentKey, childKey)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	err = json.Unmarshal([]byte(dataS), &fsdata)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	// only active accounts can be marked as deleted
	if fsdata.Status != "active" || fsdata.DeleteDate != 0 {
		return nil, errf(codes.InvalidArgument, "%s", "Passing File System Status")
	}
	// send delete to the group store
	fsdata.Status = "deleted"
	fsdata.DeleteDate = time.Now().Unix()
	dataB, err = json.Marshal(fsdata)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	// write updated information into the group store
	_, err = s.fsws.writeGStore(parentKey, childKey, dataB)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}

	// Prep things to return
	status = "OK"
	result = fmt.Sprintf("filesystem %s in account %s was deleted", r.FSid, r.Acctnum)
	return &fb.DeleteFSResponse{Payload: result, Status: status}, nil
}

// UpdateFS ...
func (s *FileSystemAPIServer) UpdateFS(ctx context.Context, r *fb.UpdateFSRequest) (*fb.UpdateFSResponse, error) {
	var status string
	var result string
	var acctData AcctPayLoad
	var err error
	var dataB []byte
	// Get incomming ip
	pr, ok := peer.FromContext(ctx)
	if ok {
		fmt.Println(pr.Addr)
	}
	// getAcct data
	acctData, err = s.getAcct("/acct", r.Acctnum)
	if err != nil {
		log.Printf("Error %v on lookup for account %s", err, r.Acctnum)
		return nil, err
	}
	// validate token
	if acctData.Token != r.Token {
		return nil, errf(codes.PermissionDenied, "%s", "Invalid Token")
	}
	// Setup keys to pull file system data
	parentKey := fmt.Sprintf("/acct/%s/fs", r.Acctnum)
	childKey := r.FSid

	// try and get filesystem details form the group store
	result, err = s.fsws.getGStore(parentKey, childKey)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	if result == "" {
		return nil, errf(codes.NotFound, "%s", "Account Not Found")
	}
	var fsData FileSysPayLoad
	err = json.Unmarshal([]byte(result), &fsData)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	// update account information
	if r.Filesys.Name != "" && fsData.Status == "active" {
		// Check for duplicate Name
		err = s.dupNameCheck(r.Acctnum, r.Filesys.Name)
		if err != nil {
			return nil, errf(codes.FailedPrecondition, "%v", err)
		}
		fsData.Name = r.Filesys.Name
	}
	if r.Filesys.Status != "" {
		if fsData.Status == "deleted" && r.Filesys.Status != "deleted" {
			fsData.DeleteDate = 0
		}
		fsData.Status = r.Filesys.Status
	}
	// write new information to the group store
	dataB, err = json.Marshal(fsData)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	// write information into the group store
	_, err = s.fsws.writeGStore(parentKey, childKey, dataB)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	// Pull updated data
	var uresult string
	uresult, err = s.fsws.getGStore(parentKey, childKey)
	if err != nil {
		return nil, errf(codes.Internal, "%v", err)
	}
	if uresult == "" {
		return nil, errf(codes.NotFound, "%s", "Update Not Found")
	}

	// DO stuff
	status = "OK"
	return &fb.UpdateFSResponse{Payload: uresult, Status: status}, nil
}

// GrantAddrFS ...
func (s *FileSystemAPIServer) GrantAddrFS(ctx context.Context, r *fb.GrantAddrFSRequest) (*fb.GrantAddrFSResponse, error) {
	var status string
	var err error
	var acctData AcctPayLoad
	var fsData FileSysPayLoad
	var addrData AddrPayLoad
	var dataB []byte
	// Get incomming ip
	pr, ok := peer.FromContext(ctx)
	if ok {
		fmt.Println(pr.Addr)
	}
	// getAcct data
	acctData, err = s.getAcct("/acct", r.Acctnum)
	if err != nil {
		log.Printf("Error %v on lookup for account %s", err, r.Acctnum)
		return nil, err
	}
	// validate token
	if acctData.Token != r.Token {
		return nil, errf(codes.PermissionDenied, "%s", "Invalid Token")
	}
	// getFS data
	fs := fmt.Sprintf("/acct/%s/fs", r.Acctnum)
	fsData, err = s.getFS(fs, r.FSid)
	if err != nil {
		log.Printf("Error %v on lookup for File system %s", err, r.Acctnum)
		return nil, err
	}
	if fsData.Status == "active" {
		log.Println("FileSystem is active")
	}
	// write out the ip address
	parentKey := fmt.Sprintf("/fs/%s/addr", r.FSid)
	childKey := r.Addr

	parentKeyA, parentKeyB := murmur3.Sum128([]byte(parentKey))
	childKeyA, childKeyB := murmur3.Sum128([]byte(childKey))
	timestampMicro := brimtime.TimeToUnixMicro(time.Now())
	addrData.Addr = r.Addr
	dataB, err = json.Marshal(addrData)
	if err != nil {
		log.Printf("Marshal Error: %v\n...", err)
		return nil, errf(codes.Internal, "%v", err)
	}
	_, err = s.fsws.gstore.Write(context.Background(), parentKeyA, parentKeyB, childKeyA, childKeyB, timestampMicro, dataB)
	if err != nil {
		log.Printf("Write Error: %v", err)
		return nil, errf(codes.Internal, "%v", err)
	}

	// DO stuff
	status = fmt.Sprintf("addr %s for filesystem %s with account id %s was granted", r.Addr, r.FSid, r.Acctnum)
	return &fb.GrantAddrFSResponse{Status: status}, nil
}

// RevokeAddrFS ...
func (s *FileSystemAPIServer) RevokeAddrFS(ctx context.Context, r *fb.RevokeAddrFSRequest) (*fb.RevokeAddrFSResponse, error) {
	var status string
	var err error
	var acctData AcctPayLoad
	// Get incomming ip
	pr, ok := peer.FromContext(ctx)
	if ok {
		fmt.Println(pr.Addr)
	}
	// getAcct data
	acctData, err = s.getAcct("/acct", r.Acctnum)
	if err != nil {
		log.Printf("Error %v on lookup for account %s", err, r.Acctnum)
		return nil, errf(codes.NotFound, "%v", err)
	}
	// validate token
	if acctData.Token != r.Token {
		return nil, errf(codes.PermissionDenied, "%s", "Invalid Token")
	}
	parentKey := fmt.Sprintf("/fs/%s/addr", r.FSid)
	childKey := r.Addr

	parentKeyA, parentKeyB := murmur3.Sum128([]byte(parentKey))
	childKeyA, childKeyB := murmur3.Sum128([]byte(childKey))
	timestampMicro := brimtime.TimeToUnixMicro(time.Now())
	// Delete addr
	_, err = s.fsws.gstore.Delete(context.Background(), parentKeyA, parentKeyB, childKeyA, childKeyB, timestampMicro)
	if store.IsNotFound(err) {
		log.Printf("/fs/%s/addr/%s did not exist to delete", r.FSid, r.Addr)
		return nil, errf(codes.NotFound, "%s", "Addr not found")
	} else if err != nil {
		return nil, errf(codes.Internal, "%s", err)
	}
	// DO stuff
	status = fmt.Sprintf("addr %s for filesystem %s with account id %s was revoked", r.Addr, r.FSid, r.Acctnum)
	return &fb.RevokeAddrFSResponse{Status: status}, nil
}

// getAcct ...
func (s *FileSystemAPIServer) getAcct(k string, ck string) (AcctPayLoad, error) {
	var a AcctPayLoad
	var err error

	data, err := s.fsws.getGStore(k, ck)
	if err != nil {
		return a, errf(codes.Internal, "%v", err)
	}
	if data == "" {
		return a, errf(codes.NotFound, "%s", "Account Not Found")
	}
	log.Printf("Got back %s", data)
	err = json.Unmarshal([]byte(data), &a)
	if err != nil {
		return a, errf(codes.Internal, "%v", err)
	}
	if a.Status != "active" {
		return a, errf(codes.NotFound, "%s", "Account Not Active")
	}
	return a, nil
}

// LookupAddrFS ...
func (s *FileSystemAPIServer) LookupAddrFS(ctx context.Context, r *fb.LookupAddrFSRequest) (*fb.LookupAddrFSResponse, error) {
	var err error
	var status string

	// Check if IP is assigned to the file system
	parentKey := fmt.Sprintf("/fs/%s/addr", r.FSid)
	pKeyA, pKeyB := murmur3.Sum128([]byte(parentKey))
	cKeyA, cKeyB := murmur3.Sum128([]byte(r.Addr))

	_, _, err = s.fsws.gstore.Read(context.Background(), pKeyA, pKeyB, cKeyA, cKeyB, nil)
	if store.IsNotFound(err) {
		log.Printf("/fs/%s/addr/%s did not exist to delete", r.FSid, r.Addr)
		return nil, errf(codes.NotFound, "%s", "Addr not found")
	} else if err != nil {
		return nil, errf(codes.Internal, "%s", err)
	}
	//
	// Verify Account is activea
	status = "OK"
	return &fb.LookupAddrFSResponse{Status: status}, nil
}

// getAcct ...
func (s *FileSystemAPIServer) getFS(k string, ck string) (FileSysPayLoad, error) {
	var fs FileSysPayLoad
	var err error

	data, err := s.fsws.getGStore(k, ck)
	if err != nil {
		return fs, errf(codes.Internal, "%v", err)
	}
	if data == "" {
		return fs, errf(codes.NotFound, "%s", "FileSystem Not Found")
	}
	log.Printf("Got back %s", data)
	err = json.Unmarshal([]byte(data), &fs)
	if err != nil {
		return fs, errf(codes.Internal, "%v", err)
	}
	if fs.Status != "active" {
		return fs, errf(codes.NotFound, "%s", "Account Not Active")
	}
	return fs, nil
}

// dupNameCheck ...
func (s *FileSystemAPIServer) dupNameCheck(fs string, n string) error {
	var fsdata FileSysPayLoad
	// try and get file system details form the group store
	data, err := s.fsws.readGroupGStore(fs)
	log.Printf("Data from the list: %v", data)
	if err != nil {
		log.Printf("Problem talking to Group Store: %v", err)
		return err
	}
	if data == "" {
		return nil
	}
	fsList := strings.Split(data, "|")
	log.Printf("Number of file systems in the list: %v", len(fsList))
	log.Printf("File System: %v", fsList)
	for i := 0; i < len(fsList); i++ {
		if fsList[i] != "" {
			err = json.Unmarshal([]byte(fsList[i]), &fsdata)
			if err != nil {
				log.Printf("Unmarshal Error: %v", err)
				return err
			}
			if fsdata.Status == "active" {
				if strings.ToLower(fsdata.Name) == strings.ToLower(n) {
					log.Printf("FileSystem Name already exists: %s", n)
					return errors.New("Name already exists")
				}
			}
		}
	}
	return nil
}

// addrList ...
func (s *FileSystemAPIServer) addrList(fsid string) ([]string, error) {
	var ap AddrPayLoad
	pKey := fmt.Sprintf("/fs/%s/addr", fsid)
	pKeyA, pKeyB := murmur3.Sum128([]byte(pKey))
	items, err := s.fsws.gstore.ReadGroup(context.Background(), pKeyA, pKeyB)
	if store.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	l := make([]string, len(items))
	for k, v := range items {
		err = json.Unmarshal(v.Value, &ap)
		if err != nil {
			return nil, err
		}
		l[k] = ap.Addr
	}
	return l, nil
}
