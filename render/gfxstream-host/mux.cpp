#include "mux.h"
#include <algorithm>
#include <atomic>
#include <cerrno>
#include <cstdint>
#include <cstdio>
#include <fcntl.h>
#include <map>
#include <memory>
#include <mutex>
#include <poll.h>
#include <thread>
#include <unistd.h>
#include <vector>

namespace {
constexpr size_t MaxPayload = 1024 * 1024;
constexpr size_t MaxChannels = 64;
enum Kind : uint16_t {Open=1, Data=2, Close=3, Reply=4, Closed=5, Error=6, Opened=7, Ping=8, Pong=9, Ready=10};
uint32_t get32(const uint8_t* b) {return uint32_t(b[0]) | uint32_t(b[1])<<8 | uint32_t(b[2])<<16 | uint32_t(b[3])<<24;}
void put32(uint8_t* b,uint32_t v) {for(int i=0;i<4;++i)b[i]=uint8_t(v>>(i*8));}
// A stalled client must not retain a remote GPU session indefinitely. Pings
// keep an otherwise idle session alive; partial frames have the same deadline.
int readExact(void* output,size_t length,bool allowEOF=false) {
    auto* b=static_cast<uint8_t*>(output);size_t done=0;
    while(done<length) {
        pollfd p{STDIN_FILENO,POLLIN,0};int ready;
        do {ready=poll(&p,1,30000);}while(ready<0&&errno==EINTR);
        if(ready<=0)return -1;
        const auto n=read(STDIN_FILENO,b+done,length-done);
        if(n<0&&errno==EINTR)continue;
        if(n==0)return allowEOF&&done==0?0:-1;
        if(n<0)return -1;
        done+=size_t(n);
    }
    return 1;
}
bool writeExact(int fd,const void* input,size_t length) {
    auto* b=static_cast<const uint8_t*>(input);
    while(length) {
        pollfd p{fd,POLLOUT,0};int ready;
        do{ready=poll(&p,1,30000);}while(ready<0&&errno==EINTR);
        if(ready<=0)return false;
        auto n=write(fd,b,length);
        if(n<0&&(errno==EINTR||errno==EAGAIN))continue;
        if(n<=0)return false;b+=n;length-=size_t(n);
    }
    return true;
}
struct Writer {
    int fd;std::mutex mutex;std::atomic<bool> failed{false};
    explicit Writer(int output):fd(output){}
    bool frame(Kind kind,uint32_t channel,const char* bytes=nullptr,size_t size=0) {
        if(size>MaxPayload)return false;
        uint8_t header[16]={'O','M','G','R',1,0,uint8_t(kind),0};put32(header+8,channel);put32(header+12,uint32_t(size));
        std::lock_guard<std::mutex> guard(mutex);
        if(failed)return false;
        if(!writeExact(fd,header,sizeof(header))||!writeExact(fd,bytes,size)){failed=true;return false;}
        return true;
    }
};
struct Channel {
    gfxstream::RenderChannelPtr native;std::thread reader;
    void stop(){native->stop();if(reader.joinable())reader.join();}
};
}

int serveMux(const gfxstream::RendererPtr& renderer,int output,const std::string& ready) {
    int flags=fcntl(output,F_GETFL);
    if(flags<0||fcntl(output,F_SETFL,flags|O_NONBLOCK)<0)return 1;
    Writer writer(output);std::map<uint32_t,std::unique_ptr<Channel>> channels;
    if(!writer.frame(Ready,0,ready.data(),ready.size()))return 1;
    int result=0;
    for(;;) {
        uint8_t header[16];int status=readExact(header,sizeof(header),true);
        if(status==0)break;
        if(status<0||header[0]!='O'||header[1]!='M'||header[2]!='G'||header[3]!='R'||header[4]!=1||header[5]!=0||header[7]!=0){result=1;break;}
        const auto kind=Kind(header[6]);const uint32_t id=get32(header+8),size=get32(header+12);
        if(size>MaxPayload){result=1;break;}
        std::vector<char> bytes(size);if(size&&readExact(bytes.data(),size)<0){result=1;break;}
        if(kind==Ping&&id==0&&size==0){if(!writer.frame(Pong,0)){result=1;break;}continue;}
        if(!id){result=1;break;}
        auto found=channels.find(id);
        if(kind==Open&&size==0&&found==channels.end()&&channels.size()<MaxChannels) {
            auto channel=std::make_unique<Channel>();channel->native=renderer->createRenderChannel(nullptr,1);
            if(!channel->native||!writer.frame(Opened,id)){result=1;break;}
            auto native=channel->native;
            channel->reader=std::thread([native,&writer,id]{
                gfxstream::RenderChannel::Buffer reply;
                for(;;) {
                    auto status=native->tryRead(&reply);
                    if(status==gfxstream::RenderChannel::IoResult::TryAgain){native->waitUntilReadable();continue;}
                    if(status!=gfxstream::RenderChannel::IoResult::Ok)break;
                    for(size_t offset=0;offset<reply.size();) {
                        const size_t count=std::min(MaxPayload,reply.size()-offset);
                        if(!writer.frame(Reply,id,reply.data()+offset,count)){native->stop();return;}
                        offset+=count;
                    }
                }
                writer.frame(Closed,id);
            });
            channels.emplace(id,std::move(channel));
        } else if(kind==Data&&found!=channels.end()) {
            gfxstream::RenderChannel::Buffer packet(bytes.begin(),bytes.end());
            for(;;) {
                auto status=found->second->native->tryWrite(std::move(packet));
                if(status==gfxstream::RenderChannel::IoResult::Ok)break;
                if(status!=gfxstream::RenderChannel::IoResult::TryAgain){result=1;break;}
                found->second->native->waitUntilWritable();
            }
            if(result)break;
        } else if(kind==Close&&size==0) {
            if(found!=channels.end()){found->second->stop();channels.erase(found);}
        } else {result=1;break;}
        if(writer.failed){result=1;break;}
    }
    if(result){const std::string error="invalid, exhausted or stalled graphics session";writer.frame(Error,0,error.data(),error.size());}
    // Stop every channel before joining any reader: each may be waiting for
    // operations on another channel of this same Vulkan device.
    for(auto& entry:channels)entry.second->native->stop();
    for(auto& entry:channels)entry.second->stop();
    return writer.failed?1:result;
}
