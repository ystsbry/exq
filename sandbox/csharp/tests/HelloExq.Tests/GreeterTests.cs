using HelloExq;

namespace HelloExq.Tests;

public class GreeterTests
{
    [Fact]
    public void Greet_WithName_ReturnsPersonalizedGreeting()
    {
        Assert.Equal("Hello, exq!", Greeter.Greet("exq"));
    }

    [Fact]
    public void Greet_TrimsSurroundingWhitespace()
    {
        Assert.Equal("Hello, exq!", Greeter.Greet("  exq  "));
    }

    // test-filter コマンドの例として、名前で絞り込めるテストを複数用意しておく。
    // 例: exq run test-filter -- "FullyQualifiedName~WithoutName"
    [Theory]
    [InlineData(null)]
    [InlineData("")]
    [InlineData("   ")]
    public void Greet_WithoutName_FallsBackToWorld(string? name)
    {
        Assert.Equal("Hello, world!", Greeter.Greet(name));
    }
}
