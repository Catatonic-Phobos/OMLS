// OMLS host adapter. The upstream Gfxstream decoder executes the command
// stream; this adapter supplies authenticated-SSH-compatible stdio transport.
#include "render-utils/render_api.h"
#include "mux.h"
#include <atomic>
#include <cerrno>
#include <csignal>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <iostream>
#include <string>
#include <thread>
#include <unistd.h>

static bool writeAll(int fd, const char* data, size_t size) {
    while (size) {
        const auto n = write(fd, data, size);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) return false;
        data += n; size -= n;
    }
    return true;
}
static std::string jsonString(const char* value) {
    std::string out = "\"";
    for (const unsigned char c : std::string(value ? value : "")) {
        if (c == '"' || c == '\\') {out += '\\'; out += c;}
        else if (c < 32) {char escaped[7]; snprintf(escaped, sizeof(escaped), "\\u%04x", c); out += escaped;}
        else out += c;
    }
    return out + "\"";
}
int main(int argc, char** argv) {
    bool probe = false, channelMode = false, muxMode = false, trace = false;
    std::string icd, expected;
    for (int i = 1; i < argc; ++i) {
        const std::string arg(argv[i]);
        if (arg == "--probe") probe = true;
        else if (arg == "--stdio") channelMode = true;
        else if (arg == "--mux") muxMode = true;
        else if (arg == "--trace") trace = true;
        else if (arg == "--icd" && i+1 < argc) icd = argv[++i];
        else if (arg == "--expect" && i+1 < argc) expected = argv[++i];
        else {std::cerr << "invalid host argument\n"; return 2;}
    }
    if (int(probe)+int(channelMode)+int(muxMode)!=1 || icd.empty()) {
        std::cerr << "usage: omls-gfxstream-host (--probe|--stdio|--mux) --icd FILE [--expect DEVICE]\n"; return 2;
    }
    if (access(icd.c_str(), R_OK) != 0) {perror("Vulkan ICD"); return 1;}
    setenv("VK_DRIVER_FILES", icd.c_str(), 1);
    setenv("VK_ICD_FILENAMES", icd.c_str(), 1);
    if (trace) setenv("ANDROID_EMU_VK_LOG_CALLS", "1", 1);
    signal(SIGPIPE, SIG_IGN);
    // Keep upstream diagnostics out of the binary command/reply channel.
    const int protocol = dup(STDOUT_FILENO);
    if (protocol < 0 || dup2(STDERR_FILENO, STDOUT_FILENO) < 0) return 1;
    setvbuf(stdout, nullptr, _IONBF, 0);
    auto library = gfxstream::initLibrary();
    if (!library) return 1;
    library->setRenderer(SELECTED_RENDERER_HOST);
    gfxstream::host::FeatureSet features;
    features.Vulkan.setEnabled(true);
    features.GuestVulkanOnly.setEnabled(true);
    auto renderer = library->initRenderer(1280, 720, features, false);
    if (!renderer) {std::cerr << "Gfxstream Vulkan initialization failed\n"; return 1;}
    char *name = nullptr, *driver = nullptr;
    uint32_t driverVersion = 0, api = 0, vendor = 0, device = 0, type = 0;
    uint64_t memory = 0;
    if (!renderer->getVulkanEmulationDeviceInfo(&name, &driver, &driverVersion, &api, &vendor, &device, &type, &memory)
        || !name || type == 4 || (expected.size() && std::string(name).find(expected) == std::string::npos)) {
        std::cerr << "Hardware Vulkan device did not match requested executor\n";
        renderer->stop(true); return 1;
    }
    int result = 0;
    if (!probe) renderer->onGuestGraphicsProcessCreate(1);
    const std::string report = "{\"backend\":\"gfxstream\",\"backend_initialized\":true,\"device\":" + jsonString(name)
            + ",\"driver\":" + jsonString(driver) + ",\"api_version\":" + std::to_string(api)
            + ",\"driver_version\":" + std::to_string(driverVersion) + ",\"vendor_id\":" + std::to_string(vendor)
            + ",\"device_id\":" + std::to_string(device) + ",\"device_local_heap_bytes\":" + std::to_string(memory)
            + ",\"executor_pid\":" + std::to_string(getpid())
            + ",\"multiplexed_session_ready\":true,\"protocol_version\":1,\"application_icd_ready\":false,\"presentation_ready\":false}\n";
    if (probe) {
        if (!writeAll(protocol, report.data(), report.size())) result = 1;
    } else if(muxMode) {result=serveMux(renderer,protocol,report);} else {
        auto channel = renderer->createRenderChannel(nullptr, 1);
        if (!channel) {renderer->stop(true); return 1;}
        std::atomic<bool> failed{false};
        std::thread replies([&] {
            gfxstream::RenderChannel::Buffer reply;
            for (;;) {
                const auto status = channel->tryRead(&reply);
                if (status == gfxstream::RenderChannel::IoResult::TryAgain) {channel->waitUntilReadable(); continue;}
                if (status != gfxstream::RenderChannel::IoResult::Ok) break;
                if (!writeAll(protocol, reply.data(), reply.size())) {failed = true; channel->stop(); break;}
            }
        });
        char bytes[64*1024];
        while (!failed) {
            const auto n = read(STDIN_FILENO, bytes, sizeof(bytes));
            if (n < 0 && errno == EINTR) continue;
            if (n < 0) failed = true;
            if (n <= 0) break;
            gfxstream::RenderChannel::Buffer packet(bytes, bytes+n);
            for (;;) {
                const auto status = channel->tryWrite(std::move(packet));
                if (status == gfxstream::RenderChannel::IoResult::Ok) break;
                if (status != gfxstream::RenderChannel::IoResult::TryAgain) {failed = true; break;}
                channel->waitUntilWritable();
            }
        }
        channel->stop(); replies.join(); result = failed ? 1 : 0;
    }
    free(name); free(driver);
    if (!probe) renderer->cleanupProcGLObjects(1);
    renderer->stop(true); renderer.reset(); library.reset(); close(protocol);
    return result;
}
