def add(a, b):
    return a + b


def divide(a, b):
    if b == 0:
        raise ValueError("Cannot divide by zero")
    return a / b


def mean(xs):
    total = 0
    for x in xs:
        total = add(total, x)
    return divide(total, len(xs))
