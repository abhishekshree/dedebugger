#include <stdio.h>

int sum (int a, int b) {
    return a + b;
}

int main(int argc, char const *argv[])
{
    int a = 1;
    int b = 2;
    int c = sum(a, b);
    printf("sum of %d and %d is %d\n", a, b, c);
    return 0;
}
