package tflite

import "fmt"

// BuiltinOp is schema.fbs BuiltinOperator.
type BuiltinOp int32

// The builtin operators this package knows by name (schema.fbs numbering).
const (
	OpAdd             BuiltinOp = 0
	OpAveragePool2D   BuiltinOp = 1
	OpConcatenation   BuiltinOp = 2
	OpConv2D          BuiltinOp = 3
	OpDepthwiseConv2D BuiltinOp = 4
	OpDepthToSpace    BuiltinOp = 5
	OpDequantize      BuiltinOp = 6
	OpFullyConnected  BuiltinOp = 9
	OpLogistic        BuiltinOp = 14
	OpMaxPool2D       BuiltinOp = 17
	OpMul             BuiltinOp = 18
	OpRelu            BuiltinOp = 19
	OpRelu6           BuiltinOp = 21
	OpReshape         BuiltinOp = 22
	OpResizeBilinear  BuiltinOp = 23
	OpSoftmax         BuiltinOp = 25
	OpTanh            BuiltinOp = 28
	OpCustom          BuiltinOp = 32
	OpPad             BuiltinOp = 34
	OpGather          BuiltinOp = 36
	OpTranspose       BuiltinOp = 39
	OpMean            BuiltinOp = 40
	OpSub             BuiltinOp = 41
	OpDiv             BuiltinOp = 42
	OpSqueeze         BuiltinOp = 43
	OpStridedSlice    BuiltinOp = 45
	OpExp             BuiltinOp = 47
	OpSplit           BuiltinOp = 49
	OpCast            BuiltinOp = 53
	OpPrelu           BuiltinOp = 54
	OpMaximum         BuiltinOp = 55
	OpMinimum         BuiltinOp = 57
	OpPadV2           BuiltinOp = 60
	OpSlice           BuiltinOp = 65
	OpTransposeConv   BuiltinOp = 67
	OpTile            BuiltinOp = 69
	OpExpandDims      BuiltinOp = 70
	OpSum             BuiltinOp = 74
	OpSqrt            BuiltinOp = 75
	OpRsqrt           BuiltinOp = 76
	OpShape           BuiltinOp = 77
	OpPow             BuiltinOp = 78
	OpReduceMax       BuiltinOp = 82
	OpPack            BuiltinOp = 83
	OpUnpack          BuiltinOp = 88
	OpSquare          BuiltinOp = 92
	OpResizeNearest   BuiltinOp = 97
	OpLeakyRelu       BuiltinOp = 98
	OpSquaredDiff     BuiltinOp = 99
	OpSplitV          BuiltinOp = 102
	OpHardSwish       BuiltinOp = 117
	OpBatchMatMul     BuiltinOp = 126
	OpGelu            BuiltinOp = 150
)

var opNames = map[BuiltinOp]string{
	OpAdd: "ADD", OpAveragePool2D: "AVERAGE_POOL_2D", OpConcatenation: "CONCATENATION",
	OpConv2D: "CONV_2D", OpDepthwiseConv2D: "DEPTHWISE_CONV_2D", OpDepthToSpace: "DEPTH_TO_SPACE",
	OpDequantize: "DEQUANTIZE", OpFullyConnected: "FULLY_CONNECTED", OpLogistic: "LOGISTIC",
	OpMaxPool2D: "MAX_POOL_2D", OpMul: "MUL", OpRelu: "RELU", OpRelu6: "RELU6", OpReshape: "RESHAPE",
	OpResizeBilinear: "RESIZE_BILINEAR", OpSoftmax: "SOFTMAX", OpTanh: "TANH", OpCustom: "CUSTOM",
	OpPad: "PAD", OpGather: "GATHER", OpTranspose: "TRANSPOSE", OpMean: "MEAN", OpSub: "SUB",
	OpDiv: "DIV", OpSqueeze: "SQUEEZE", OpStridedSlice: "STRIDED_SLICE", OpExp: "EXP",
	OpSplit: "SPLIT", OpCast: "CAST", OpPrelu: "PRELU", OpMaximum: "MAXIMUM", OpMinimum: "MINIMUM",
	OpPadV2: "PADV2", OpSlice: "SLICE", OpTransposeConv: "TRANSPOSE_CONV", OpTile: "TILE",
	OpExpandDims: "EXPAND_DIMS", OpSum: "SUM", OpSqrt: "SQRT", OpRsqrt: "RSQRT", OpShape: "SHAPE",
	OpPow: "POW", OpReduceMax: "REDUCE_MAX", OpPack: "PACK", OpUnpack: "UNPACK", OpSquare: "SQUARE",
	OpResizeNearest: "RESIZE_NEAREST_NEIGHBOR", OpLeakyRelu: "LEAKY_RELU",
	OpSquaredDiff: "SQUARED_DIFFERENCE", OpSplitV: "SPLIT_V", OpHardSwish: "HARD_SWISH",
	OpBatchMatMul: "BATCH_MATMUL", OpGelu: "GELU",
}

func (o BuiltinOp) String() string {
	if s, ok := opNames[o]; ok {
		return s
	}
	return fmt.Sprintf("BUILTIN_%d", int32(o))
}
