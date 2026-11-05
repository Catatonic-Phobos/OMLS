#pragma once
#include "render-utils/Renderer.h"
#include <string>

int serveMux(const gfxstream::RendererPtr& renderer, int output, const std::string& ready);
