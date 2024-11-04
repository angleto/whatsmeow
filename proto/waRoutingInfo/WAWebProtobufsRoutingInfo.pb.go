// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.35.1
// 	protoc        v3.21.12
// source: waRoutingInfo/WAWebProtobufsRoutingInfo.proto

package waRoutingInfo

import (
	reflect "reflect"
	sync "sync"

	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"

	_ "embed"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type RoutingInfo struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	RegionID     []int32 `protobuf:"varint,1,rep,name=regionID" json:"regionID,omitempty"`
	ClusterID    []int32 `protobuf:"varint,2,rep,name=clusterID" json:"clusterID,omitempty"`
	TaskID       *int32  `protobuf:"varint,3,opt,name=taskID" json:"taskID,omitempty"`
	Debug        *bool   `protobuf:"varint,4,opt,name=debug" json:"debug,omitempty"`
	TcpBbr       *bool   `protobuf:"varint,5,opt,name=tcpBbr" json:"tcpBbr,omitempty"`
	TcpKeepalive *bool   `protobuf:"varint,6,opt,name=tcpKeepalive" json:"tcpKeepalive,omitempty"`
}

func (x *RoutingInfo) Reset() {
	*x = RoutingInfo{}
	mi := &file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *RoutingInfo) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RoutingInfo) ProtoMessage() {}

func (x *RoutingInfo) ProtoReflect() protoreflect.Message {
	mi := &file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use RoutingInfo.ProtoReflect.Descriptor instead.
func (*RoutingInfo) Descriptor() ([]byte, []int) {
	return file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescGZIP(), []int{0}
}

func (x *RoutingInfo) GetRegionID() []int32 {
	if x != nil {
		return x.RegionID
	}
	return nil
}

func (x *RoutingInfo) GetClusterID() []int32 {
	if x != nil {
		return x.ClusterID
	}
	return nil
}

func (x *RoutingInfo) GetTaskID() int32 {
	if x != nil && x.TaskID != nil {
		return *x.TaskID
	}
	return 0
}

func (x *RoutingInfo) GetDebug() bool {
	if x != nil && x.Debug != nil {
		return *x.Debug
	}
	return false
}

func (x *RoutingInfo) GetTcpBbr() bool {
	if x != nil && x.TcpBbr != nil {
		return *x.TcpBbr
	}
	return false
}

func (x *RoutingInfo) GetTcpKeepalive() bool {
	if x != nil && x.TcpKeepalive != nil {
		return *x.TcpKeepalive
	}
	return false
}

var File_waRoutingInfo_WAWebProtobufsRoutingInfo_proto protoreflect.FileDescriptor

//go:embed WAWebProtobufsRoutingInfo.pb.raw
var file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDesc []byte

var (
	file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescOnce sync.Once
	file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescData = file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDesc
)

func file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescGZIP() []byte {
	file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescOnce.Do(func() {
		file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescData = protoimpl.X.CompressGZIP(file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescData)
	})
	return file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDescData
}

var file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_msgTypes = make([]protoimpl.MessageInfo, 1)
var file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_goTypes = []any{
	(*RoutingInfo)(nil), // 0: WAWebProtobufsRoutingInfo.RoutingInfo
}
var file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_depIdxs = []int32{
	0, // [0:0] is the sub-list for method output_type
	0, // [0:0] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_init() }
func file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_init() {
	if File_waRoutingInfo_WAWebProtobufsRoutingInfo_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   1,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_goTypes,
		DependencyIndexes: file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_depIdxs,
		MessageInfos:      file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_msgTypes,
	}.Build()
	File_waRoutingInfo_WAWebProtobufsRoutingInfo_proto = out.File
	file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_rawDesc = nil
	file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_goTypes = nil
	file_waRoutingInfo_WAWebProtobufsRoutingInfo_proto_depIdxs = nil
}
