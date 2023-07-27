package main

/*
#cgo CFLAGS: -IC:/ffmpeg/include
#cgo LDFLAGS: -LC:/ffmpeg -lavformat-60 -lswresample-4  -lswscale-7 -lavutil-58 -lavcodec-60
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <libavutil/samplefmt.h>
#include <libavutil/timestamp.h>
#include <libavformat/avformat.h>
#include <libswresample/swresample.h>
#include <libswscale/swscale.h>

void * PointerAt(void ** array, int index) {
	return array[index];
}

struct AVStream * StreamAt(struct AVFormatContext *context, int index) {
	if (index < 0 || index >= context->nb_streams) {
		return NULL;
	}
	return context->streams[index];
}

unsigned char * DataAt(struct AVFrame *frame, int index) {
	return frame->data[index];
}

void FreePacket(struct AVPacket *packet) {
	av_packet_free(&packet);
}
*/
import "C"
import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
)

const MAX_PACKET_QUEUE_SIZE = 300

func main() {
	sdl.Init(sdl.INIT_VIDEO | sdl.INIT_EVENTS)
	defer sdl.Quit()

	window, err := sdl.CreateWindow(
		"Video Player",
		sdl.WINDOWPOS_UNDEFINED,
		sdl.WINDOWPOS_UNDEFINED,
		1920,
		1024,
		sdl.WINDOW_SHOWN,
	)
	defer window.Destroy()
	if err != nil {
		fmt.Printf("Failed to create window: %s\n", err)
		return
	}

	renderer, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED)
	defer renderer.Destroy()
	if err != nil {
		fmt.Printf("Failed to create renderer: %s\n", err)
		return
	}

	texture, err := renderer.CreateTexture(
		sdl.PIXELFORMAT_IYUV,
		sdl.TEXTUREACCESS_STREAMING,
		1920,
		1024,
	)
	defer texture.Destroy()
	if err != nil {
		fmt.Printf("Failed to create texture: %s\n", err)
		return
	}

	renderer.SetDrawColor(0, 0, 0, 0)
	renderer.Clear()

	filename := "D:\\gallery\\TulipPlayground\\Princess.Mononoke.mp4"

	cFilename := C.CString(filename)
	defer C.free(unsafe.Pointer(cFilename))

	formatContextPtr := C.avformat_alloc_context()
	defer C.avformat_free_context(formatContextPtr)

	didOpen := C.avformat_open_input(&formatContextPtr, cFilename, nil, nil)
	if didOpen < 0 {
		fmt.Printf("Could not open file '%s'\n", filename)
		return
	}

	streamIndex := C.av_find_best_stream(formatContextPtr, C.AVMEDIA_TYPE_VIDEO, -1, -1, nil, 0)
	if streamIndex < 0 {
		fmt.Printf("Could not find video stream in the input, aborting\n")
		return
	}

	stream := C.StreamAt(formatContextPtr, streamIndex)
	if stream == nil {
		fmt.Printf("Could not find video stream in the input, aborting\n")
		return
	}

	decoder := C.avcodec_find_decoder(stream.codecpar.codec_id)
	if decoder == nil {
		fmt.Printf("Failed to find codec\n")
		return
	}

	decoderContext := C.avcodec_alloc_context3(decoder)
	defer C.avcodec_free_context(&decoderContext)
	if decoderContext == nil {
		fmt.Printf("Failed to allocate the decoder context\n")
		return
	}

	didSetParameters := C.avcodec_parameters_to_context(decoderContext, stream.codecpar)
	if didSetParameters < 0 {
		fmt.Printf("Failed to copy decoder parameters to input decoder context\n")
		return
	}

	didOpenCodec := C.avcodec_open2(decoderContext, decoder, nil)
	if didOpenCodec < 0 {
		fmt.Printf("Failed to open codec\n")
		return
	}

	packetQueue := make(chan *C.AVPacket, 1000)
	go streamPackets(packetQueue, formatContextPtr, streamIndex)
	go decodePackets(packetQueue, decoderContext, texture, renderer)

	running := true
	for running {
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch event.(type) {
			case *sdl.QuitEvent:
				running = false
				break
			}
		}
	}
}

func decodePackets(packetQueue chan *C.AVPacket, decoderContext *C.AVCodecContext, texture *sdl.Texture, renderer *sdl.Renderer) {
	for {
		if len(packetQueue) == 0 {
			println("No packets to decode")
			continue
		}

		packet := <-packetQueue

		// Don't forget to call avcodec_flush_buffers() when seeking

		didSend := C.avcodec_send_packet(decoderContext, packet)
		if didSend < 0 {
			// How to check the different error codes?
			fmt.Printf("Failed to send packet to decoder\n")
			continue
		}

		frame := C.av_frame_alloc()
		// defer C.av_frame_free(&frame)

		didReceive := C.avcodec_receive_frame(decoderContext, frame)
		if didReceive < 0 {
			fmt.Printf("Failed to receive frame from decoder\n")
			C.av_frame_free(&frame)
			continue
		}

		texture.UpdateYUV(
			nil,
			DataToByteSlice(C.DataAt(frame, 0), int(frame.linesize[0])),
			int(frame.linesize[0]),
			DataToByteSlice(C.DataAt(frame, 1), int(frame.linesize[1])),
			int(frame.linesize[1]),
			DataToByteSlice(C.DataAt(frame, 2), int(frame.linesize[2])),
			int(frame.linesize[2]),
		)

		renderer.Copy(texture, nil, nil)
		renderer.Present()

		fmt.Printf("Frame received: %dx%d\n", frame.width, frame.height)
		C.av_frame_free(&frame)

		C.FreePacket(packet)
	}
}

func DataToByteSlice(data *C.uchar, length int) []byte {
	var bytes []byte
	sliceHeader := (*reflect.SliceHeader)(unsafe.Pointer(&bytes))
	sliceHeader.Data = uintptr(unsafe.Pointer(data))
	sliceHeader.Len = length
	sliceHeader.Cap = length
	return bytes
}

func streamPackets(packetQueue chan *C.AVPacket, formatContext *C.AVFormatContext, streamIndex C.int) {
	for {
		if len(packetQueue) >= 300 {
			continue
		}

		packetPtr := C.av_packet_alloc()

		didReadPacket := C.av_read_frame(formatContext, packetPtr)
		if didReadPacket < 0 {
			fmt.Printf("Failed to read packet from stream\n")
			continue
		}

		// Only send packets from the video stream
		if packetPtr.stream_index != streamIndex {
			C.av_packet_free(&packetPtr)
			continue
		}

		// packetClonePtr := C.av_packet_clone(packetPtr)
		// if packetClonePtr == nil {
		// 	fmt.Printf("Failed to clone packet\n")
		// 	continue
		// }

		packetQueue <- packetPtr
	}
}
