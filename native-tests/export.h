#ifndef MIO_TEST_EXPORT_H
#define MIO_TEST_EXPORT_H
#include <stdint.h>
#include <stddef.h>
#ifdef _WIN32
#define API __declspec(dllexport)
#else
#define API __attribute__((visibility("default")))
#endif
#endif
